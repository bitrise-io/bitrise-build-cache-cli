package xcode

import (
	"crypto/sha256"
	"encoding/hex"
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
	AbsolutePath string    `json:"path"`
	Size         int64     `json:"size"`
	Hash         string    `json:"hash"`
	ModTime      time.Time `json:"modTime"`
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
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		fileInfo, err := os.Stat(path)
		if err != nil {
			return err
		}
		fileSize := fileInfo.Size()
		modTime := fileInfo.ModTime()

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
			Size:         fileSize,
			Hash:         hash,
			ModTime:      modTime,
		})

		return nil
	})

	if err != nil {
		return DerivedData{}, err
	}

	logger.Infof("(i) Processed %d DerivedData files", len(dd.Files))

	return dd, nil
}
