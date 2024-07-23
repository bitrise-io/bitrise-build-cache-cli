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
	InputFiles       []*FileInfo            `json:"inputFiles"`
	InputDirectories []*DirectoryInfo       `json:"inputDirectories"`
	DerivedData      CacheDirectoryMetadata `json:"derivedData"`
	XcodeCacheDir    CacheDirectoryMetadata `json:"xcodeCacheDir"`
	CacheKey         string                 `json:"cacheKey"`
	CreatedAt        time.Time              `json:"createdAt"`
	AppID            string                 `json:"appId,omitempty"`
	BuildID          string                 `json:"buildId,omitempty"`
	WorkspaceID      string                 `json:"workspaceId,omitempty"`
	GitCommit        string                 `json:"gitCommit,omitempty"`
	GitBranch        string                 `json:"gitBranch,omitempty"`
}

type CreateMetadataParams struct {
	ProjectRootDirPath string
	DerivedDataPath    string
	XcodeCacheDirPath  string
	CacheKey           string
}

func CreateMetadata(params CreateMetadataParams, envProvider func(string) string, logger log.Logger) (*Metadata, error) {
	fileInfos, dirInfos, err := calculateFileInfos(params.ProjectRootDirPath, logger)
	if err != nil {
		return nil, fmt.Errorf("calculate file infos: %w", err)
	}

	var derivedData CacheDirectoryMetadata
	if params.DerivedDataPath != "" {
		derivedData, err = calculateCacheDirectoryInfo(params.DerivedDataPath, logger)
		if err != nil {
			return nil, fmt.Errorf("calculate derived data info: %w", err)
		}
	}

	var xcodeCacheDir CacheDirectoryMetadata
	if params.XcodeCacheDirPath != "" {
		xcodeCacheDir, err = calculateCacheDirectoryInfo(params.XcodeCacheDirPath, logger)
		if err != nil {
			return nil, fmt.Errorf("calculate xcode cache dir info: %w", err)
		}
	}

	m := Metadata{
		InputFiles:       fileInfos,
		InputDirectories: dirInfos,
		DerivedData:      derivedData,
		XcodeCacheDir:    xcodeCacheDir,
		CacheKey:         params.CacheKey,
		CreatedAt:        time.Now(),
		AppID:            envProvider("BITRISE_APP_SLUG"),
		BuildID:          envProvider("BITRISE_BUILD_SLUG"),
		GitCommit:        envProvider("BITRISE_GIT_COMMIT"),
		GitBranch:        envProvider("BITRISE_GIT_BRANCH"),
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

func RestoreDirectoryInfos(dirInfos []*DirectoryInfo, rootDir string, logger log.Logger) error {
	for _, dir := range dirInfos {
		path := filepath.Join(rootDir, dir.Path)
		if err := os.MkdirAll(path, os.ModePerm); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}

		if err := os.Chtimes(path, dir.ModTime, dir.ModTime); err != nil {
			return fmt.Errorf("set directory mod time: %w", err)
		}
	}

	logger.Infof("(i) Restored %d directory infos", len(dirInfos))

	return nil
}

func RestoreFileInfos(fileInfos []*FileInfo, rootDir string, logger log.Logger) (int, error) {
	updated := 0

	logger.Infof("(i) %d files' info loaded from cache metadata", len(fileInfos))

	for _, fi := range fileInfos {
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
			continue
		}

		if err = os.Chmod(fi.Path, fi.Mode); err != nil {
			logger.Debugf("Error setting file mode time for %s: %v", fi.Path, err)
			continue
		}

		err = setAttributes(fi.Path, fi.Attributes)
		if err != nil {
			logger.Debugf("Error setting file attributes for %s: %v", fi.Path, err)
			continue
		}

		updated++
	}

	logger.Infof("(i) %d files' modification time restored", updated)

	return updated, nil
}
