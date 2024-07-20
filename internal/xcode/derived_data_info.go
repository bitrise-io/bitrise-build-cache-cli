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
	Files []*DerivedDataFile `json:"files"`
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
		if d.IsDir() {
			return nil
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
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		fileInfo, err := os.Stat(path)
		if err != nil {
			return err
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
			Size:         fileInfo.Size(),
			Hash:         hash,
			ModTime:      fileInfo.ModTime(),
			Mode:         fileInfo.Mode(),
		})

		return nil
	})

	if err != nil {
		return DerivedData{}, err
	}

	logger.Infof("(i) Processed %d DerivedData files", len(dd.Files))

	return dd, nil
}
