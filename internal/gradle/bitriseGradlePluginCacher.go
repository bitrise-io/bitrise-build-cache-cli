package gradle

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/go-utils/v2/log"
)

var (
	errFmtPluginsCache       = "failed to cache plugins: %w"
	errFmtPluginsFromKVCache = "failed to download plugins from cache: %w"
	errFmtPluginsToKVCache   = "failed to upload plugins to cache: %w"
)

type BitrisePluginCacher struct{}

func (pluginCacher BitrisePluginCacher) CachePlugins(
	ctx context.Context,
	kvClient *kv.Client,
	logger log.Logger,
	plugins []BitriseGradlePlugin,
) error {
	var errs []error

	logger.Infof("(i) Fetching Bitrise Gradle plugins")

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4) // Limit parallelization
	for _, plugin := range plugins {
		wg.Add(1)
		semaphore <- struct{}{}

		logger.Infof("(i) Fetching " + plugin.id + ":" + plugin.version)

		go func(plugin BitriseGradlePlugin) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release a slot in the semaphore

			// Try to fetch from cache
			if downloaded, err := pluginCacher.fetchFromCache(ctx, kvClient, plugin); err == nil {
				if !downloaded {
					logger.Debugf("(i) " + plugin.id + ":" + plugin.version + " fetched from kv cache")
				} else {
					logger.Debugf("(i) " + plugin.id + ":" + plugin.version + " was already in the local repository")
				}
				return
			}

			// Try to download from artifact repositories
			downloader := GradlePluginDownloader{
				groupID:         bitriseGradlePluginGroup,
				artifactID:      plugin.id,
				artifactVersion: plugin.version,
			}
			sources, err := downloader.Download()
			if err != nil {
				errs = append(errs, err)
				return
			}

			sourcesUsed := ""
			for key := range sources {
				if sourcesUsed != "" {
					sourcesUsed += ", "
				}
				sourcesUsed += key
			}

			logger.Debugf("(i) " + plugin.id + ":" + plugin.version + " fetched from artifact repositories: " + sourcesUsed)

			// Upload to cache if fetched from repositories
			if err := pluginCacher.cache(ctx, kvClient, plugin); err != nil {
				errs = append(errs, err)
				return
			}

		}(plugin)
	}

	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf(errFmtPluginsCache, errs[0])
	}

	return nil
}

func (pluginCacher BitrisePluginCacher) fetchFromCache(
	ctx context.Context,
	kvClient *kv.Client,
	plugin BitriseGradlePlugin,
) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("PWD")
	}
	destination := filepath.Join(home, ".m2", "repository")

	var downloaded = false
	for _, file := range plugin.files() {
		neededFetch, err := kvClient.DownloadFile(
			ctx,
			filepath.Join(destination, file.path()),
			file.key(),
			0,
			true,
			true,
			false,
		)

		downloaded = downloaded || neededFetch

		if err != nil {
			return downloaded, fmt.Errorf(errFmtPluginsFromKVCache, err)
		}
	}

	return downloaded, nil
}

func (pluginCacher BitrisePluginCacher) cache(
	ctx context.Context,
	kvClient *kv.Client,
	plugin BitriseGradlePlugin,
) error {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("PWD")
	}
	destination := filepath.Join(home, ".m2", "repository")

	for _, file := range plugin.files() {
		err := kvClient.UploadFileToBuildCache(
			ctx,
			filepath.Join(destination, file.path()),
			file.key(),
		)

		if err != nil {
			return fmt.Errorf(errFmtPluginsToKVCache, err)
		}
	}

	return nil
}
