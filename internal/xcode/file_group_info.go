package xcode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/pkg/xattr"

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

	mc := NewMetadataCollector(logger)
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10) // Limit parallelization

	err := filepath.WalkDir(cacheDirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		wg.Add(1)
		semaphore <- struct{}{} // Block if there are too many goroutines are running

		go func(d fs.DirEntry) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release a slot in the semaphore

			if err := collectMetadata(path, d, mc, collectAttributes, rootDir, logger); err != nil {
				logger.Errorf("Failed to collect metadata: %s", err)
			}
		}(d)

		return nil
	})

	wg.Wait()

	if err != nil {
		return FileGroupInfo{}, fmt.Errorf("walk dir: %w", err)
	}

	dd.Files = mc.Files
	dd.Directories = mc.Dirs

	logger.Infof("(i) Collected %d files and %d directories ", len(dd.Files), len(dd.Directories))
	logger.Debugf("(i) Largest processed file size: %s", humanize.Bytes(uint64(mc.LargestFileSize)))

	return dd, nil
}

func collectMetadata(path string, d fs.DirEntry, mc *MetadataCollector, collectAttributes bool, rootDir string, logger log.Logger) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("get absolute path: %w", err)
	}
	inf, err := d.Info()
	if err != nil {
		return fmt.Errorf("get file info: %w", err)
	}

	if d.IsDir() {
		mc.AddDir(&DirectoryInfo{
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
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("hash copy file content: %w", err)
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

	mc.AddFile(&FileInfo{
		Path:       savedPath,
		Size:       inf.Size(),
		Hash:       hash,
		ModTime:    inf.ModTime(),
		Mode:       inf.Mode(),
		Attributes: attrs,
	})

	return nil
}

func getAttributes(path string) (map[string]string, error) {
	attributes := make(map[string]string)
	attrNames, err := xattr.List(path)
	if err != nil {
		return nil, fmt.Errorf("list attributes: %w", err)
	}

	for _, attr := range attrNames {
		value, err := xattr.Get(path, attr)
		if err != nil {
			return nil, fmt.Errorf("xattr get: %w", err)
		}
		attributes[attr] = string(value)
	}

	return attributes, nil
}

func setAttributes(path string, attributes map[string]string) error {
	for attr, value := range attributes {
		if err := xattr.Set(path, attr, []byte(value)); err != nil {
			return fmt.Errorf("xattr set: %w", err)
		}
	}

	return nil
}
