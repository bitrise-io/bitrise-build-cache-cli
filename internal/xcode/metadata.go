package xcode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/filegroup"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/dustin/go-humanize"
)

const metadataVersion = 1

type Metadata struct {
	ProjectFiles         filegroup.Info `json:"projectFiles"`
	DerivedData          filegroup.Info `json:"derivedData"`
	XcodeCacheDir        filegroup.Info `json:"xcodeCacheDir"`
	CacheKey             string         `json:"cacheKey"`
	CreatedAt            time.Time      `json:"createdAt"`
	AppID                string         `json:"appId,omitempty"`
	BuildID              string         `json:"buildId,omitempty"`
	WorkspaceID          string         `json:"workspaceId,omitempty"`
	GitCommit            string         `json:"gitCommit,omitempty"`
	GitBranch            string         `json:"gitBranch,omitempty"`
	BuildCacheCLIVersion string         `json:"cliVersion,omitempty"`
	MetadataVersion      int            `json:"metadataVersion"`
}

type CreateMetadataParams struct {
	ProjectRootDirPath string
	DerivedDataPath    string
	XcodeCacheDirPath  string
	CacheKey           string
	FollowSymlinks     bool
	SkipSPM            bool
}

func CreateMetadata(params CreateMetadataParams, envProvider func(string) string, logger log.Logger) (*Metadata, error) {
	if params.ProjectRootDirPath == "" {
		return nil, fmt.Errorf("missing project root directory path")
	}
	var projectFiles filegroup.Info
	projectFiles, err := filegroup.CollectFileGroupInfo(params.ProjectRootDirPath,
		true,
		params.FollowSymlinks,
		false,
		logger)
	if err != nil {
		return nil, fmt.Errorf("calculate project files info: %w", err)
	}

	var derivedData filegroup.Info
	if params.DerivedDataPath != "" {
		derivedData, err = filegroup.CollectFileGroupInfo(params.DerivedDataPath,
			false,
			params.FollowSymlinks,
			params.SkipSPM,
			logger)
		if err != nil {
			return nil, fmt.Errorf("calculate derived data info: %w", err)
		}
	}

	var xcodeCacheDir filegroup.Info
	if params.XcodeCacheDirPath != "" {
		xcodeCacheDir, err = filegroup.CollectFileGroupInfo(params.XcodeCacheDirPath,
			false,
			params.FollowSymlinks,
			params.SkipSPM,
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

func RestoreDirectoryInfos(dirInfos []*filegroup.DirectoryInfo, rootDir string, logger log.Logger) error {
	for _, dir := range dirInfos {
		if err := filegroup.RestoreDirectoryInfo(*dir, rootDir); err != nil {
			return fmt.Errorf("restoring directory info: %w", err)
		}
	}

	logger.Infof("(i) Restored %d directory infos", len(dirInfos))

	return nil
}

func RestoreSymlinks(symlinks []*filegroup.SymlinkInfo, logger log.Logger) (int, error) {
	updated := 0

	logger.Infof("(i) %d symlinks' info loaded from cache metadata", len(symlinks))

	for _, si := range symlinks {
		if filegroup.RestoreSymlink(*si, logger) {
			updated++
		}
	}

	logger.Infof("(i) %d symlinks restored", updated)

	return updated, nil
}

func RestoreFileInfos(fileInfos []*filegroup.FileInfo, rootDir string, logger log.Logger) (int, error) {
	updated := 0

	logger.Infof("(i) %d files' info loaded from cache metadata", len(fileInfos))

	for _, fi := range fileInfos {
		if filegroup.RestoreFileInfo(*fi, rootDir, logger) {
			updated++
		}
	}

	logger.Infof("(i) %d files' modification time restored", updated)

	return updated, nil
}

func DeleteFileGroup(fgi filegroup.Info, logger log.Logger) {
	logger.Infof("Deleting %d files", len(fgi.Files))

	for _, file := range fgi.Files {
		if err := os.Remove(file.Path); err != nil {
			logger.Infof("Failed to remove file %s: %s", file.Path, err)
		}
	}
}

func (md *Metadata) Print(logger log.Logger, isDebugLogMode bool) {
	logger.Infof("Cache metadata:")
	logger.Infof("  Cache key: %s", md.CacheKey)
	createdAt := ""
	if !md.CreatedAt.IsZero() {
		createdAt = md.CreatedAt.String()
	}
	logger.Infof("  Created at: %s", createdAt)
	logger.Infof("  App ID: %s", md.AppID)
	logger.Infof("  Build ID: %s", md.BuildID)
	logger.Infof("  Git commit: %s", md.GitCommit)
	logger.Infof("  Git branch: %s", md.GitBranch)
	logger.Infof("  Project files: %d", len(md.ProjectFiles.Files))
	logger.Infof("  Project symlinks: %d", len(md.ProjectFiles.Symlinks))
	logger.Infof("  DerivedData files: %d", len(md.DerivedData.Files))
	logger.Infof("  DerivedData symlinks: %d", len(md.DerivedData.Symlinks))
	logger.Infof("  Xcode cache files: %d", len(md.XcodeCacheDir.Files))
	logger.Infof("  Xcode cache symlinks: %d", len(md.XcodeCacheDir.Symlinks))
	logger.Infof("  Build Cache CLI version: %s", md.BuildCacheCLIVersion)
	logger.Infof("  Metadata version: %d", md.MetadataVersion)

	if isDebugLogMode {
		sortedDDFiles := make([]*filegroup.FileInfo, len(md.DerivedData.Files))
		copy(sortedDDFiles, md.DerivedData.Files)

		sort.Slice(sortedDDFiles, func(i, j int) bool {
			return sortedDDFiles[i].Size > sortedDDFiles[j].Size
		})

		if len(sortedDDFiles) > 10 {
			sortedDDFiles = sortedDDFiles[:10]
		}

		logger.Debugf("  Largest files:")
		for i, file := range sortedDDFiles {
			//nolint: gosec
			logger.Debugf("    %d. %s (%s)", i+1, file.Path, humanize.Bytes(uint64(file.Size)))
		}
	}
}
