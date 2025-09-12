package gradle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/filegroup"
)

const metadataVersion = 1

type Metadata struct {
	ConfigCacheFiles     filegroup.Info `json:"configCacheFiles"`
	CacheKey             string         `json:"cacheKey"`
	OS                   string         `json:"os"`
	CreatedAt            time.Time      `json:"createdAt"`
	AppID                string         `json:"appId,omitempty"`
	BuildID              string         `json:"buildId,omitempty"`
	WorkspaceID          string         `json:"workspaceId,omitempty"`
	GitCommit            string         `json:"gitCommit,omitempty"`
	GitBranch            string         `json:"gitBranch,omitempty"`
	BuildCacheCLIVersion string         `json:"cliVersion,omitempty"`
	MetadataVersion      int            `json:"metadataVersion"`
}

func (g *Cache) CreateMetadata(cacheKey string, dir string) (*Metadata, error) {
	fg, err := filegroup.CollectFileGroupInfo(dir,
		true,
		false,
		false,
		g.logger)
	if err != nil {
		return nil, fmt.Errorf("calculate project files info: %w", err)
	}

	m := Metadata{
		ConfigCacheFiles:     fg,
		CacheKey:             cacheKey,
		OS:                   runtime.GOOS,
		CreatedAt:            time.Now(),
		AppID:                g.envProvider["BITRISE_APP_SLUG"],
		BuildID:              g.envProvider["BITRISE_BUILD_SLUG"],
		GitCommit:            g.envProvider["BITRISE_GIT_COMMIT"],
		GitBranch:            g.envProvider["BITRISE_GIT_BRANCH"],
		BuildCacheCLIVersion: g.envProvider["BITRISE_BUILD_CACHE_CLI_VERSION"],
		MetadataVersion:      metadataVersion,
	}

	if m.GitCommit == "" {
		m.GitCommit = g.envProvider["GIT_CLONE_COMMIT_HASH"]
	}

	return &m, nil
}

func (g *Cache) SaveMetadata(metadata *Metadata, fileName string) (int64, error) {
	jsonData, err := json.Marshal(metadata)
	if err != nil {
		return 0, fmt.Errorf("encoding JSON: %w", err)
	}

	dir := filepath.Dir(fileName)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return 0, fmt.Errorf("create cache metadata directory: %w", err)
	}

	err = os.WriteFile(fileName, jsonData, 0o600)
	if err != nil {
		return 0, fmt.Errorf("writing JSON file: %w", err)
	}

	g.logger.Infof("(i) Metadata saved to %s", fileName)

	return int64(len(jsonData)), nil
}

func (g *Cache) LoadMetadata(file string) (*Metadata, int64, error) {
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

func (md *Metadata) Print(logger log.Logger) {
	logger.Infof("Cache metadata:")
	logger.Infof("  Cache key: %s", md.CacheKey)
	createdAt := ""
	if !md.CreatedAt.IsZero() {
		createdAt = md.CreatedAt.String()
	}
	logger.Infof("  Created at: %s", createdAt)
	logger.Infof("  OS: %s", md.OS)
	logger.Infof("  App ID: %s", md.AppID)
	logger.Infof("  Build ID: %s", md.BuildID)
	logger.Infof("  Git commit: %s", md.GitCommit)
	logger.Infof("  Git branch: %s", md.GitBranch)
	logger.Infof("  Config cache files: %d", len(md.ConfigCacheFiles.Files))
	logger.Infof("  Build Cache CLI version: %s", md.BuildCacheCLIVersion)
	logger.Infof("  Metadata version: %d", md.MetadataVersion)
}
