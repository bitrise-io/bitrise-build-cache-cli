package xcode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

const metadataVersion = 1

type Metadata struct {
	ProjectFiles         FileGroupInfo `json:"projectFiles"`
	DerivedData          FileGroupInfo `json:"derivedData"`
	XcodeCacheDir        FileGroupInfo `json:"xcodeCacheDir"`
	CacheKey             string        `json:"cacheKey"`
	CreatedAt            time.Time     `json:"createdAt"`
	AppID                string        `json:"appId,omitempty"`
	BuildID              string        `json:"buildId,omitempty"`
	WorkspaceID          string        `json:"workspaceId,omitempty"`
	GitCommit            string        `json:"gitCommit,omitempty"`
	GitBranch            string        `json:"gitBranch,omitempty"`
	BuildCacheCLIVersion string        `json:"cliVersion,omitempty"`
	MetadataVersion      int           `json:"metadataVersion"`
}

type CreateMetadataParams struct {
	ProjectRootDirPath string
	DerivedDataPath    string
	XcodeCacheDirPath  string
	CacheKey           string
	FollowSymlinks     bool
}

func CreateMetadata(params CreateMetadataParams, envProvider func(string) string, logger log.Logger) (*Metadata, error) {
	if params.ProjectRootDirPath == "" {
		return nil, fmt.Errorf("missing project root directory path")
	}
	var projectFiles FileGroupInfo
	projectFiles, err := collectFileGroupInfo(params.ProjectRootDirPath,
		true,
		params.FollowSymlinks,
		logger)
	if err != nil {
		return nil, fmt.Errorf("calculate project files info: %w", err)
	}

	var derivedData FileGroupInfo
	if params.DerivedDataPath != "" {
		derivedData, err = collectFileGroupInfo(params.DerivedDataPath,
			false,
			params.FollowSymlinks,
			logger)
		if err != nil {
			return nil, fmt.Errorf("calculate derived data info: %w", err)
		}
	}

	var xcodeCacheDir FileGroupInfo
	if params.XcodeCacheDirPath != "" {
		xcodeCacheDir, err = collectFileGroupInfo(params.XcodeCacheDirPath,
			false,
			params.FollowSymlinks,
			logger)
		if err != nil {
			return nil, fmt.Errorf("calculate xcode cache dir info: %w", err)
		}
	}

	m := Metadata{
		ProjectFiles:         projectFiles,
		DerivedData:          derivedData,
		XcodeCacheDir:        xcodeCacheDir,
		CacheKey:             params.CacheKey,
		CreatedAt:            time.Now(),
		AppID:                envProvider("BITRISE_APP_SLUG"),
		BuildID:              envProvider("BITRISE_BUILD_SLUG"),
		GitCommit:            envProvider("BITRISE_GIT_COMMIT"),
		GitBranch:            envProvider("BITRISE_GIT_BRANCH"),
		BuildCacheCLIVersion: envProvider("BITRISE_BUILD_CACHE_CLI_VERSION"),
		MetadataVersion:      metadataVersion,
	}

	if m.GitCommit == "" {
		m.GitCommit = envProvider("GIT_CLONE_COMMIT_HASH")
	}

	return &m, nil
}

func SaveMetadata(metadata *Metadata, fileName string, logger log.Logger) (int64, error) {
	if fileName == "" {
		return 0, fmt.Errorf("missing output fileName")
	}

	jsonData, err := json.Marshal(metadata)
	if err != nil {
		return 0, fmt.Errorf("encoding JSON: %w", err)
	}

	dir := filepath.Dir(fileName)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return 0, fmt.Errorf("create cache metadata directory: %w", err)
	}

	// Write JSON data to a file
	err = os.WriteFile(fileName, jsonData, 0600)
	if err != nil {
		return 0, fmt.Errorf("writing JSON file: %w", err)
	}

	logger.Infof("(i) Metadata saved to %s", fileName)

	return int64(len(jsonData)), nil
}

func LoadMetadata(file string) (*Metadata, int64, error) {
	jsonData, err := os.ReadFile(file)
	if err != nil {
		return nil, 0, fmt.Errorf("reading %s: %w", file, err)
	}

	var metadata Metadata
	if err := json.Unmarshal(jsonData, &metadata); err != nil {
		return nil, 0, fmt.Errorf("parsing JSON: %w", err)
	}

	return &metadata, int64(len(jsonData)), nil
}

func RestoreDirectoryInfos(dirInfos []*DirectoryInfo, rootDir string, logger log.Logger) error {
	for _, dir := range dirInfos {
		if err := restoreDirectoryInfo(*dir, rootDir); err != nil {
			return fmt.Errorf("restoring directory info: %w", err)
		}
	}

	logger.Infof("(i) Restored %d directory infos", len(dirInfos))

	return nil
}

func RestoreSymlinks(symlinks []*SymlinkInfo, logger log.Logger) (int, error) {
	updated := 0

	logger.Infof("(i) %d symlinks' info loaded from cache metadata", len(symlinks))

	for _, si := range symlinks {
		if restoreSymlink(*si, logger) {
			updated++
		}
	}

	logger.Infof("(i) %d symlinks restored", updated)

	return updated, nil
}

func RestoreFileInfos(fileInfos []*FileInfo, rootDir string, logger log.Logger) (int, error) {
	updated := 0

	logger.Infof("(i) %d files' info loaded from cache metadata", len(fileInfos))

	for _, fi := range fileInfos {
		if restoreFileInfo(*fi, rootDir, logger) {
			updated++
		}
	}

	logger.Infof("(i) %d files' modification time restored", updated)

	return updated, nil
}

func DeleteFileGroup(fgi FileGroupInfo, logger log.Logger) {
	logger.Infof("Deleting %d files", len(fgi.Files))

	for _, file := range fgi.Files {
		if err := os.Remove(file.Path); err != nil {
			logger.Infof("Failed to remove file %s: %s", file.Path, err)
		}
	}
}
