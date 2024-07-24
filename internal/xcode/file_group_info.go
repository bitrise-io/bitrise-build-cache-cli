package xcode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/pkg/xattr"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

type FileGroupInfo struct {
	Files       []*FileInfo      `json:"files"`
	Directories []*DirectoryInfo `json:"directories"`
}

type DirectoryInfo struct {
	Path    string    `json:"path"`
	ModTime time.Time `json:"modTime"`
}

type FileInfo struct {
	Path       string            `json:"path"`
	Size       int64             `json:"size"`
	Hash       string            `json:"hash"`
	ModTime    time.Time         `json:"modTime"`
	Mode       os.FileMode       `json:"mode"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

func collectFileGroupInfo(cacheDirPath string, rootDir string, collectAttributes bool, logger log.Logger) (FileGroupInfo, error) {
	var dd FileGroupInfo
	var largestFileSize int64

	err := filepath.WalkDir(cacheDirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		inf, err := d.Info()
		if err != nil {
			return fmt.Errorf("get file info: %w", err)
		}

		if d.IsDir() {
			dd.Directories = append(dd.Directories, &DirectoryInfo{
				Path:    absPath,
				ModTime: inf.ModTime(),
			})
			return nil
		}

		if inf.Mode()&os.ModeSymlink != 0 {
			logger.Debugf("Skipping symbolic link: %s", path)

			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		hasher := sha256.New()
		if _, err := io.Copy(hasher, file); err != nil {
			return err
		}
		hash := hex.EncodeToString(hasher.Sum(nil))

		savedPath := absPath
		if rootDir != "" {
			relPath, err := filepath.Rel(rootDir, path)
			if err != nil {
				return fmt.Errorf("get relative path: %w", err)
			}
			savedPath = relPath
		}

		var attrs map[string]string
		if collectAttributes {
			attrs, err = getAttributes(path)
			if err != nil {
				return fmt.Errorf("getting attributes: %w", err)
			}
		}

		dd.Files = append(dd.Files, &FileInfo{
			Path:       savedPath,
			Size:       inf.Size(),
			Hash:       hash,
			ModTime:    inf.ModTime(),
			Mode:       inf.Mode(),
			Attributes: attrs,
		})

		if inf.Size() > largestFileSize {
			largestFileSize = inf.Size()
		}

		return nil
	})

	if err != nil {
		return FileGroupInfo{}, err
	}

	logger.Infof("(i) Processed %d cache directory files", len(dd.Files))
	logger.Debugf("(i) Largest processed file size: %s", humanize.Bytes(uint64(largestFileSize)))

	return dd, nil
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
