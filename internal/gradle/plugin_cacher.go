package gradle

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
)

const (
	errFmtPluginsCache       = "failed to cache plugins: %w"
	errFmtPluginsFromKVCache = "failed to download plugins from cache: %w"
	errFmtPluginsToKVCache   = "failed to upload plugins to cache: %w"
)

type PluginCacher struct{}

func (pluginCacher PluginCacher) CachePlugins(
	ctx context.Context,
	kvClient *kv.Client,
	logger log.Logger,
	plugins []Plugin,
) error {
	var errs []error

	logger.Infof("(i) Fetching Bitrise Gradle plugins")

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4) // Limit parallelization
	for _, plugin := range plugins {
		wg.Add(1)

		logger.Infof("(i) Fetching " + plugin.id + ":" + plugin.version)

		go func(plugin Plugin) {
			semaphore <- struct{}{}
			defer wg.Done()
			defer func() { <-semaphore }() // Release a slot in the semaphore

			for _, file := range plugin.files() {
				// Try to fetch from cache
				if skipped, err := pluginCacher.fetchFromCache(ctx, kvClient, file, logger); err == nil {
					if !skipped {
						logger.Debugf("(i) " + file.name() + " fetched from kv cache")
					} else {
						logger.Debugf("(i) " + file.name() + " was already in the local repository")
					}

					continue
				}

				// Try to download from artifact repositories
				downloader := PluginDownloader{logger: logger}
				source, err := downloader.Download(file)
				if err != nil {
					errs = append(errs, err)

					return
				}

				logger.Debugf("(i) " + file.name() + " fetched from artifact repositories: " + source)

				// Upload to cache if fetched from repositories
				if err := pluginCacher.cache(ctx, kvClient, file, logger); err != nil {
					errs = append(errs, err)

					return
				}
			}
		}(plugin)
	}

	wg.Wait()

	if len(errs) > 0 {
		for _, err := range errs {
			logger.Debugf("(i) error while caching plugins: %s", err.Error())
		}

		return fmt.Errorf(errFmtPluginsCache, errs[0])
	}

	return nil
}

func (pluginCacher PluginCacher) fetchFromCache(
	ctx context.Context,
	kvClient *kv.Client,
	file PluginFile,
	logger log.Logger,
) (bool, error) {
	logger.Debugf("Fetching " + file.name() + " from kv cache with key: " + file.key())
	downloaded, err := kvClient.DownloadFile(
		ctx,
		filepath.Join(file.absoluteDirPath(logger), file.name()),
		file.key(),
		0,
		true,
		true,
		false,
	)
	if err != nil {
		return downloaded, fmt.Errorf(errFmtPluginsFromKVCache, err)
	}

	return downloaded, nil
}

func (pluginCacher PluginCacher) cache(
	ctx context.Context,
	kvClient *kv.Client,
	file PluginFile,
	logger log.Logger,
) error {
	err := kvClient.UploadFileToBuildCache(
		ctx,
		filepath.Join(file.absoluteDirPath(logger), file.name()),
		file.key(),
	)
	if err != nil {
		return fmt.Errorf(errFmtPluginsToKVCache, err)
	}

	return nil
}
