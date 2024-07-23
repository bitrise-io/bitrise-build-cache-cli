package xcode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/dustin/go-humanize"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

type CacheDirectoryMetadata struct {
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

func calculateCacheDirectoryInfo(cacheDirPath string, logger log.Logger) (CacheDirectoryMetadata, error) {
	var dd CacheDirectoryMetadata
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

		// Skip symbolic links
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

		dd.Files = append(dd.Files, &FileInfo{
			Path:    absPath,
			Size:    inf.Size(),
			Hash:    hash,
			ModTime: inf.ModTime(),
			Mode:    inf.Mode(),
		})

		if inf.Size() > largestFileSize {
			largestFileSize = inf.Size()
		}

		return nil
	})

	if err != nil {
		return CacheDirectoryMetadata{}, err
	}

	logger.Infof("(i) Processed %d cache directory files", len(dd.Files))
	logger.Debugf("(i) Largest cache directory file size: %s", humanize.Bytes(uint64(largestFileSize)))

	return dd, nil
}

func RestoreDirectories(dd CacheDirectoryMetadata, logger log.Logger) error {
	for _, dir := range dd.Directories {
		if err := os.MkdirAll(dir.Path, os.ModePerm); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}

		if err := os.Chtimes(dir.Path, dir.ModTime, dir.ModTime); err != nil {
			return fmt.Errorf("set directory mod time: %w", err)
		}
	}

	logger.Infof("(i) Restored %d cache directories", len(dd.Directories))

	return nil
}
