package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/hash"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const GradleConfigCacheMetadataPath = "gradle-config-cache-metadata.json"

// nolint: gochecknoglobals
var saveGradleConfigCacheCmd = &cobra.Command{
	Use:          "save-gradle-configuration-cache",
	Short:        "Save the Gradle configuration cache directory into Bitrise Build Cache",
	Long:         `Save the contents of the Gradle configuration cache folder (used by Gradle to store task graph produced by the configuration phase) into Bitrise Build Cache.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logCurrentUserInfo(logger)

		logger.TInfof("Save the Gradle configuration cache directory into Bitrise Build Cache")

		logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

		logger.Infof("(i) Checking parameters")
		configCacheDir, _ := cmd.Flags().GetString("config-cache-dir")
		cacheKey, _ := cmd.Flags().GetString("key")

		logger.Infof("(i) Check Auth Config")
		authConfig, err := common.ReadAuthConfigFromEnvironments(os.Getenv)
		if err != nil {
			return fmt.Errorf("read auth config from environments: %w", err)
		}

		err = saveGradleConfigCacheCmdFn(cmd.Context(),
			authConfig,
			configCacheDir,
			cacheKey,
			logger,
			os.Getenv,
			func(name string, v ...string) (string, error) {
				output, err := exec.Command(name, v...).Output()

				return string(output), err
			})
		if err != nil {
			return fmt.Errorf("save Gradle config cache into Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ Configuration cache saved into Bitrise Build Cache ")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(saveGradleConfigCacheCmd)

	saveGradleConfigCacheCmd.Flags().String("key", "", "The cache key to use for the saved cache item (set to the Bitrise app's slug and current git branch by default)")
	saveGradleConfigCacheCmd.Flags().String("config-cache-dir", "./.gradle/configuration-cache", "Path to the Gradle configuration cache folder. It's usually the $PROJECT_ROOT/.gradle/configuration-cache")
}

func saveGradleConfigCacheCmdFn(ctx context.Context,
	authConfig common.CacheAuthConfig,
	configCacheDir,
	providedCacheKey string,
	logger log.Logger,
	envProvider func(string) string,
	commandFunc func(string, ...string) (string, error)) error {
	var err error

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

	cache := gradle.NewCache(logger, envProvider, kvClient)

	var cacheKey string
	if providedCacheKey == "" {
		logger.Infof("(i) Cache key is not explicitly specified, setting it based on the current Bitrise app's slug and git branch...")
		if cacheKey, err = cache.GetCacheKey(gradle.CacheKeyParams{}); err != nil {
			return fmt.Errorf("get cache key: %w", err)
		}
	} else {
		cacheKey = providedCacheKey
	}
	logger.Infof("(i) Cache key: %s", cacheKey)

	absDir, err := filepath.Abs(configCacheDir)
	if err != nil {
		return fmt.Errorf("get absolute path of config cache dir: %w", err)
	}

	logger.TInfof(fmt.Sprintf("Gathering metadata for cache files in %s", absDir))
	metadata, err := cache.CreateMetadata(cacheKey, absDir)
	if err != nil {
		return fmt.Errorf("create metadata: %w", err)
	}

	logger.TInfof("Saving metadata file %s", GradleConfigCacheMetadataPath)
	_, err = cache.SaveMetadata(metadata, GradleConfigCacheMetadataPath)
	if err != nil {
		return fmt.Errorf("save metadata: %w", err)
	}

	mdChecksum, err := hash.ChecksumOfFile(GradleConfigCacheMetadataPath)
	mdChecksumReader := strings.NewReader(mdChecksum)
	if err != nil {
		return fmt.Errorf("checksum of metadata file: %w", err)
	}

	logger.TInfof("Uploading cache files")

	_, err = kvClient.UploadFileGroupToBuildCache(ctx, metadata.ConfigCacheFiles)
	if err != nil {
		return fmt.Errorf("upload cache files to build cache: %w", err)
	}

	logger.TInfof("Uploading metadata checksum of %s (%s) for key %s", GradleConfigCacheMetadataPath, mdChecksum, cacheKey)
	if err := kvClient.UploadStreamToBuildCache(ctx, mdChecksumReader, cacheKey, mdChecksumReader.Size()); err != nil {
		return fmt.Errorf("upload metadata checksum to build cache: %w", err)
	}

	logger.TInfof("Uploading metadata content of %s for key %s", GradleConfigCacheMetadataPath, mdChecksum)
	if err := kvClient.UploadFileToBuildCache(ctx, GradleConfigCacheMetadataPath, mdChecksum); err != nil {
		return fmt.Errorf("upload metadata content to build cache: %w", err)
	}

	if providedCacheKey == "" {
		fallbackCacheKey, err := cache.GetCacheKey(gradle.CacheKeyParams{IsFallback: true})
		if err != nil {
			logger.Warnf("Failed to get fallback cache key: %s", err)
		} else if fallbackCacheKey != "" && cacheKey != fallbackCacheKey {
			cacheKey = fallbackCacheKey
			mdChecksumReader = strings.NewReader(mdChecksum) // reset reader
			logger.TInfof("Uploading metadata checksum of %s (%s) for fallback key %s", GradleConfigCacheMetadataPath, mdChecksum, cacheKey)
			if err := kvClient.UploadStreamToBuildCache(ctx, mdChecksumReader, cacheKey, mdChecksumReader.Size()); err != nil {
				return fmt.Errorf("upload metadata checksum to build cache: %w", err)
			}
		}
	}

	return nil
}
