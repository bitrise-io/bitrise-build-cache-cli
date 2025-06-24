package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/filegroup"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// nolint: gochecknoglobals
var restoreGradleConfigCacheCmd = &cobra.Command{
	Use:          "restore-gradle-configuration-cache",
	Short:        "Restore the Gradle configuration cache directory from Bitrise Build Cache",
	Long:         `Restore the contents of the Gradle configuration cache folder (used by Gradle to store task graph produced by the configuration phase) from Bitrise Build Cache.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logCurrentUserInfo(logger)

		logger.TInfof("Restore the Gradle configuration cache directory from Bitrise Build Cache")

		logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

		logger.Infof("(i) Checking parameters")
		cacheKey, _ := cmd.Flags().GetString("key")

		logger.Infof("(i) Check Auth Config")
		authConfig, err := common.ReadAuthConfigFromEnvironments(os.Getenv)
		if err != nil {
			return fmt.Errorf("read auth config from environments: %w", err)
		}

		err = restoreGradleConfigCacheCmdFn(cmd.Context(),
			authConfig,
			cacheKey,
			logger,
			os.Getenv,
			func(name string, v ...string) (string, error) {
				output, err := exec.Command(name, v...).Output()

				return string(output), err
			},
			pathutil.NewPathModifier(),
			utils.DefaultOsProxy())
		if err != nil {
			return fmt.Errorf("restore Gradle config cache from Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ Configuration cache restored from Bitrise Build Cache ")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(restoreGradleConfigCacheCmd)

	restoreGradleConfigCacheCmd.Flags().String("key", "", "The cache key used for the saved cache item (set to the Bitrise app's slug and current git branch by default)")
}

func checkGradleCachePathExists(logger log.Logger,
	pathModifier pathutil.PathModifier,
	osProxy utils.OsProxy) error {
	// Check if ~/.gradle/caches/**/transforms files exist, if not, fail as config cache will
	// make the build fail otherwise.
	gradleHome, err := pathModifier.AbsPath(gradleHomeNonExpanded)
	if err != nil {
		return fmt.Errorf("expand Gradle home path (%s), error: %w", gradleHomeNonExpanded, err)
	}
	dirToCheck := filepath.Join(gradleHome, "caches")
	logger.Debugf("Checking gradle cache directories in: %s", dirToCheck)
	dirs, err := osProxy.ListDirectories(dirToCheck)
	if err == nil && len(dirs) > 0 {
		logger.Debugf("Found gradle cache directories: %s", strings.Join(dirs, ", "))
		dirToCheck = filepath.Join(dirToCheck, dirs[0], "transforms")
		logger.Debugf("Checking gradle cache directories in: %s", dirToCheck)

		dirs, err = osProxy.ListDirectories(dirToCheck)
	}

	if len(dirs) == 0 || err != nil {
		logger.Errorf("No Gradle cache directories found in %s. "+
			"Make sure to run a build with the Save Gradle Cache step added and "+
			"with save transforms enabled before using configuration cache.", dirToCheck)

		if err != nil {
			return fmt.Errorf("list directories in %s: %w", dirToCheck, err)
		}

		return fmt.Errorf("no Gradle caches found in %s", dirToCheck)
	}

	return nil
}

func restoreGradleConfigCacheCmdFn(ctx context.Context,
	authConfig common.CacheAuthConfig,
	providedCacheKey string,
	logger log.Logger,
	envProvider func(string) string,
	commandFunc func(string, ...string) (string, error),
	pathModifier pathutil.PathModifier,
	osProxy utils.OsProxy) error {
	if err := checkGradleCachePathExists(logger, pathModifier, osProxy); err != nil {
		return fmt.Errorf("check Gradle cache path exists: %w", err)
	}

	kvClient, err := createKVClient(ctx,
		CreateKVClientParams{
			CacheOperationID: uuid.NewString(),
			ClientName:       ClientNameGradleConfigCache,
			AuthConfig:       authConfig,
			EnvProvider:      envProvider,
			CommandFunc:      commandFunc,
			Logger:           logger,
		})
	if err != nil {
		return fmt.Errorf("create kv client: %w", err)
	}

	g := gradle.NewCache(logger, envProvider, kvClient)

	logger.TInfof("(i) Restoring Gradle configuration cache")

	_, _, err = downloadGradleConfigCacheMetadata(ctx, GradleConfigCacheMetadataPath, providedCacheKey, g, kvClient, logger)
	if err != nil {
		return fmt.Errorf("download cache metadata: %w", err)
	}

	logger.TInfof("Loading metadata from %s", GradleConfigCacheMetadataPath)
	var metadata *gradle.Metadata
	if metadata, _, err = g.LoadMetadata(GradleConfigCacheMetadataPath); err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	metadata.Print(logger)

	logger.TInfof("Downloading configuration cache files")
	_, err = kvClient.DownloadFileGroupFromBuildCache(ctx, metadata.ConfigCacheFiles, isDebugLogMode, true, false, 100)
	if err != nil {
		logger.Infof("Failed to download DerivedData files, clearing")
		// To prevent the build from failing
		for _, file := range metadata.ConfigCacheFiles.Files {
			if err := os.Remove(file.Path); err != nil {
				logger.Infof("Failed to remove file %s: %s", file.Path, err)
			}
		}

		return fmt.Errorf("download config cache files: %w", err)
	}

	updated := 0

	logger.Infof("(i) %d files' info loaded from cache metadata", len(metadata.ConfigCacheFiles.Files))

	for _, fi := range metadata.ConfigCacheFiles.Files {
		if filegroup.RestoreFileInfo(*fi, "", logger) {
			updated++
		}
	}

	logger.Infof("(i) %d files' modification time restored", updated)

	return nil
}

func downloadGradleConfigCacheMetadata(ctx context.Context, cacheMetadataPath, providedCacheKey string,
	gradleCache *gradle.Cache,
	kvClient *kv.Client,
	logger log.Logger) (CacheKeyType, string, error) {
	var cacheKeyType CacheKeyType = CacheKeyTypeDefault
	var cacheKey string
	var err error
	if providedCacheKey == "" {
		if cacheKey, err = gradleCache.GetCacheKey(gradle.CacheKeyParams{}); err != nil {
			return "", "", fmt.Errorf("get cache key: %w", err)
		}
		logger.TInfof("Downloading cache metadata checksum for key %s", cacheKey)
	} else {
		cacheKeyType = CacheKeyTypeProvided
		cacheKey = providedCacheKey
		logger.TInfof("Downloading cache metadata checksum for provided key %s", cacheKey)
	}

	var mdChecksum strings.Builder
	err = kvClient.DownloadStreamFromBuildCache(ctx, &mdChecksum, cacheKey)
	if err != nil && !errors.Is(err, kv.ErrCacheNotFound) {
		return cacheKeyType, cacheKey, fmt.Errorf("download cache metadata checksum: %w", err)
	}

	if errors.Is(err, kv.ErrCacheNotFound) {
		cacheKeyType = CacheKeyTypeFallback
		fallbackCacheKey, fallbackErr := gradleCache.GetCacheKey(gradle.CacheKeyParams{IsFallback: true})
		if fallbackErr != nil {
			return cacheKeyType, fallbackCacheKey, errors.New("cache metadata not found in cache")
		}

		cacheKey = fallbackCacheKey
		logger.Infof("Cache metadata not found for original key, trying fallback key %s", cacheKey)

		err = kvClient.DownloadStreamFromBuildCache(ctx, &mdChecksum, cacheKey)
		if errors.Is(err, kv.ErrCacheNotFound) {
			return cacheKeyType, cacheKey, errors.New("cache metadata not found in cache")
		}
		if err != nil {
			return cacheKeyType, cacheKey, fmt.Errorf("download cache metadata checksum: %w", err)
		}
		logger.Infof("Cache metadata found for fallback key %s", cacheKey)
	}

	logger.TInfof("Downloading cache metadata content to %s for key %s", cacheMetadataPath, mdChecksum.String())
	if err := kvClient.DownloadFileFromBuildCache(ctx, cacheMetadataPath, mdChecksum.String()); err != nil {
		return cacheKeyType, cacheKey, fmt.Errorf("download cache archive: %w", err)
	}

	return cacheKeyType, cacheKey, nil
}
