package xcode

import (
	"fmt"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/pkg/xattr"
	"io/fs"
	"os"
	"path/filepath"
)

func calculateFileInfos(rootDir string, logger log.Logger) ([]*FileInfo, []*DirectoryInfo, error) {
	if rootDir == "" {
		return nil, nil, fmt.Errorf("missing rootDir")
	}

	var fileInfos []*FileInfo
	var dirInfos []*DirectoryInfo

	// Walk through the directory tree
	err := filepath.WalkDir(rootDir,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			inf, err := d.Info()
			if err != nil {
				return fmt.Errorf("get file info: %w", err)
			}

			// Skip symbolic links
			if inf.Mode()&os.ModeSymlink != 0 {
				logger.Debugf("Skipping symbolic link: %s", path)

				return nil
			}

			relPath, err := filepath.Rel(rootDir, path)
			if err != nil {
				return fmt.Errorf("get relative path: %w", err)
			}

			if d.IsDir() {
				dirInfos = append(dirInfos, &DirectoryInfo{
					Path:    relPath,
					ModTime: inf.ModTime(),
				})

				return nil
			}

			hashString, err := checksumOfFile(path)
			if err != nil {
				return fmt.Errorf("calculating hash: %w", err)
			}

			attrs, err := getAttributes(path)
			if err != nil {
				return fmt.Errorf("getting attributes: %w", err)
			}

			fileInfo := &FileInfo{
				Path:       relPath,
				Hash:       hashString,
				Size:       inf.Size(),
				ModTime:    inf.ModTime(),
				Mode:       inf.Mode(),
				Attributes: attrs,
			}

			fileInfos = append(fileInfos, fileInfo)

			return nil
		})
	if err != nil {
		return nil, nil, fmt.Errorf("calculate file infos: %w", err)
	}

	logger.Infof("(i) Processed %d input files", len(fileInfos))

	return fileInfos, dirInfos, nil
}

func getAttributes(path string) (map[string]string, error) {
	attributes := make(map[string]string)
	attrNames, err := xattr.List(path)
	if err != nil {
		return nil, err
	}

	for _, attr := range attrNames {
		value, err := xattr.Get(path, attr)
		if err != nil {
			return nil, err
		}
		attributes[attr] = string(value)
	}

	return attributes, nil
}

func setAttributes(path string, attributes map[string]string) error {
	for attr, value := range attributes {
		if err := xattr.Set(path, attr, []byte(value)); err != nil {
			return err
		}
	}
	return nil
}
