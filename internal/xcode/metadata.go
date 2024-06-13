package xcode

import (
	"encoding/json"
	"fmt"
	"github.com/bitrise-io/go-utils/v2/log"
	"os"
	"path/filepath"
)

type Metadata struct {
	FileInfos []FileInfo `json:"input_files"`
}

func SaveMetadata(rootDir string, fileName string, logger log.Logger) error {
	fileInfos, err := calculateFileInfos(rootDir, logger)
	if err != nil {
		return fmt.Errorf("failed to calculate file infos: %w", err)
	}

	if fileName == "" {
		return fmt.Errorf("missing output fileName")
	}

	m := Metadata{
		FileInfos: fileInfos,
	}

	// Encode metadata to JSON
	jsonData, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}

	dir := filepath.Dir(fileName)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create cache metadata directory: %w", err)
	}

	// Write JSON data to a file
	err = os.WriteFile(fileName, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("writing JSON file: %w", err)
	}

	logger.Infof("(i) Metadata saved to %s", fileName)

	return nil
}

func LoadMetadata(file string) (*Metadata, error) {
	jsonData, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}

	var metadata Metadata
	if err := json.Unmarshal(jsonData, &metadata); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	return &metadata, nil
}

func RestoreMTime(metadata *Metadata, rootDir string, logger log.Logger) error {
	updated := 0

	for _, fi := range metadata.FileInfos {
		path := filepath.Join(rootDir, fi.Path)

		// Skip if file doesn't exist
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}

		h, err := checksumOfFile(path)
		if err != nil {
			logger.Infof("Error hashing file %s: %v", fi.Path, err)
			continue
		}

		if h != fi.Hash {
			continue
		}

		// Set modification time
		if err := os.Chtimes(path, fi.ModTime, fi.ModTime); err != nil {
			logger.Infof("Error setting modification time for %s: %v", fi.Path, err)
		} else {
			updated++
		}
	}

	logger.Infof("(i) %d files' modification time restored", updated)

	return nil
}