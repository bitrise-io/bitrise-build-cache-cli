package xcode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

type DerivedData struct {
	Files       []*DerivedDataFile `json:"files"`
	Directories []*DerivedDataDir  `json:"directories"`
}

type DerivedDataDir struct {
	AbsolutePath string    `json:"path"`
	ModTime      time.Time `json:"modTime"`
}

type DerivedDataFile struct {
	AbsolutePath string      `json:"path"`
	Size         int64       `json:"size"`
	Hash         string      `json:"hash"`
	ModTime      time.Time   `json:"modTime"`
	Mode         os.FileMode `json:"mode"`
}

func calculateDerivedDataInfo(derivedDataPath string, logger log.Logger) (DerivedData, error) {
	var dd DerivedData

	err := filepath.WalkDir(derivedDataPath, func(path string, d fs.DirEntry, err error) error {
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
			dd.Directories = append(dd.Directories, &DerivedDataDir{
				AbsolutePath: absPath,
				ModTime:      inf.ModTime(),
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

		dd.Files = append(dd.Files, &DerivedDataFile{
			AbsolutePath: absPath,
			Size:         inf.Size(),
			Hash:         hash,
			ModTime:      inf.ModTime(),
			Mode:         inf.Mode(),
		})

		return nil
	})

	if err != nil {
		return DerivedData{}, err
	}

	logger.Infof("(i) Processed %d DerivedData files", len(dd.Files))

	return dd, nil
}

func RestoreDirectories(dd DerivedData, logger log.Logger) error {
	for _, dir := range dd.Directories {
		if err := os.MkdirAll(dir.AbsolutePath, os.ModePerm); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}

		if err := os.Chtimes(dir.AbsolutePath, dir.ModTime, dir.ModTime); err != nil {
			return fmt.Errorf("set directory mod time: %w", err)
		}
	}

	logger.Infof("(i) Restored %d DerivedData directories", len(dd.Directories))

	return nil
}
