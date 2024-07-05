package xcode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

type Metadata struct {
	FileInfos   []FileInfo `json:"inputFiles"`
	CacheKey    string     `json:"cacheKey"`
	CreatedAt   time.Time  `json:"createdAt"`
	AppID       string     `json:"appId,omitempty"`
	BuildID     string     `json:"buildId,omitempty"`
	WorkspaceID string     `json:"workspaceId,omitempty"`
	GitCommit   string     `json:"gitCommit,omitempty"`
	GitBranch   string     `json:"gitBranch,omitempty"`
}

func CreateMetadata(projectRootDir string, cacheKey string, envProvider func(string) string, logger log.Logger) (*Metadata, error) {
	fileInfos, err := calculateFileInfos(projectRootDir, logger)
	if err != nil {
		return nil, fmt.Errorf("calculate file infos: %w", err)
	}

	m := Metadata{
		FileInfos: fileInfos,
		CacheKey:  cacheKey,
		CreatedAt: time.Now(),
		AppID:     envProvider("BITRISE_APP_SLUG"),
		BuildID:   envProvider("BITRISE_BUILD_SLUG"),
		GitCommit: envProvider("BITRISE_GIT_COMMIT"),
		GitBranch: envProvider("BITRISE_GIT_BRANCH"),
	}

	if m.GitCommit == "" {
		m.GitCommit = envProvider("GIT_CLONE_COMMIT_HASH")
	}

	return &m, nil
}

func SaveMetadata(metadata *Metadata, fileName string, logger log.Logger) error {
	if fileName == "" {
		return fmt.Errorf("missing output fileName")
	}

	jsonData, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}

	dir := filepath.Dir(fileName)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("create cache metadata directory: %w", err)
	}

	// Write JSON data to a file
	err = os.WriteFile(fileName, jsonData, 0600)
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

func RestoreMTime(metadata *Metadata, rootDir string, logger log.Logger) (int, error) {
	updated := 0

	logger.Infof("(i) %d files' info loaded from cache metadata", len(metadata.FileInfos))

	for _, fi := range metadata.FileInfos {
		path := filepath.Join(rootDir, fi.Path)

		// Skip if file doesn't exist
		if _, err := os.Stat(path); os.IsNotExist(err) {
			logger.Debugf("File %s doesn't exist", fi.Path)

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
			logger.Debugf("Error setting modification time for %s: %v", fi.Path, err)
		} else {
			updated++
		}
	}

	logger.Infof("(i) %d files' modification time restored", updated)

	return updated, nil
}
