package cmd

import (
	"errors"
	"fmt"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
	"os"
	"strings"
)

// nolint: gochecknoglobals
var restoreXcodeDerivedDataFilesCmd = &cobra.Command{
	Use:   "restore-xcode-deriveddata-files",
	Short: "Restore the DerivedData folder from Bitrise Build Cache (per file)",
	Long:  `Restore the contents of the DerivedData folder (used by Xcode to store intermediate build files) from Bitrise Build Cache.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Restore Xcode DerivedData from Bitrise Build Cache")

		logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

		logger.Infof("(i) Checking parameters")
		projectRoot, _ := cmd.Flags().GetString("project-root")
		cacheKey, _ := cmd.Flags().GetString("key")

		if err := restoreXcodeDerivedDataFilesCmdFn(CacheMetadataPath, projectRoot, cacheKey, logger, os.Getenv); err != nil {
			return fmt.Errorf("restore Xcode DerivedData from Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ DerivedData restored from Bitrise Build Cache ")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(restoreXcodeDerivedDataFilesCmd)

	restoreXcodeDerivedDataFilesCmd.Flags().String("key", "", "The cache key to use for the saved cache item (set to the Bitrise app's slug and current git branch by default)")
	restoreXcodeDerivedDataFilesCmd.Flags().String("project-root", "", "Path to the iOS project folder to be built (this is used when restoring the modification time of the source files)")
	if err := restoreXcodeDerivedDataFilesCmd.MarkFlagRequired("project-root"); err != nil {
		panic(err)
	}
}

func restoreXcodeDerivedDataFilesCmdFn(cacheMetadataPath, projectRoot, providedCacheKey string, logger log.Logger, envProvider func(string) string) error {
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return fmt.Errorf("read auth config from environments: %w", err)
	}

	// Temporarily redirect all traffic to GCP
	overrideEndpointURL := consts.EndpointURLDefault
	if envProvider("BITRISE_BUILD_CACHE_ENDPOINT") != "" {
		// But still allow users to override the endpoint
		overrideEndpointURL = envProvider("BITRISE_BUILD_CACHE_ENDPOINT")
	}

	endpointURL := common.SelectEndpointURL(overrideEndpointURL, envProvider)
	logger.Infof("(i) Build Cache Endpoint URL: %s", endpointURL)

	tracker := xcode.NewStepTracker("restore-xcode-build-cache", envProvider, logger)
	defer tracker.Wait()
	// startT := time.Now()

	if err := downloadMetadata(cacheMetadataPath, providedCacheKey, endpointURL, authConfig, logger, envProvider); err != nil {
		return fmt.Errorf("download cache metadata: %w", err)
	}

	logger.TInfof("Loading metadata of the cache archive from %s", cacheMetadataPath)
	var metadata *xcode.Metadata
	if metadata, err = xcode.LoadMetadata(cacheMetadataPath); err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	logCacheMetadata(metadata, logger)

	logger.TInfof("Restoring metadata of input files")
	// var filesUpdated int
	if _, err = xcode.RestoreFileInfos(metadata.ProjectFiles.Files, projectRoot, logger); err != nil {
		return fmt.Errorf("restore modification time: %w", err)
	}

	logger.TInfof("Restoring metadata of input directories")
	if err := xcode.RestoreDirectoryInfos(metadata.ProjectFiles.Directories, projectRoot, logger); err != nil {
		return fmt.Errorf("restore metadata of input directories: %w", err)
	}

	logger.TInfof("Downloading DerivedData files")
	if err := xcode.DownloadCacheFilesFromBuildCache(metadata.DerivedData, endpointURL, authConfig, logger); err != nil {
		return fmt.Errorf("download DerivedData files: %w", err)
	}

	logger.TInfof("Restoring DerivedData directory metadata")
	if err := xcode.RestoreDirectoryInfos(metadata.DerivedData.Directories, "", logger); err != nil {
		return fmt.Errorf("restore DerivedData directories: %w", err)
	}

	if len(metadata.XcodeCacheDir.Files) > 0 {
		logger.TInfof("Downloading Xcode cache files")
		if err := xcode.DownloadCacheFilesFromBuildCache(metadata.XcodeCacheDir, endpointURL, authConfig, logger); err != nil {
			return fmt.Errorf("download Xcode cache files: %w", err)
		}

		logger.TInfof("Restoring Xcode cache directory metadata")
		if err := xcode.RestoreDirectoryInfos(metadata.XcodeCacheDir.Directories, "", logger); err != nil {
			return fmt.Errorf("restore Xcode cache directories: %w", err)
		}
	}

	return nil
}

func downloadMetadata(cacheMetadataPath, providedCacheKey, endpointURL string,
	authConfig common.CacheAuthConfig,
	logger log.Logger,
	envProvider func(string) string) error {
	var cacheKey string
	var err error
	if providedCacheKey == "" {
		if cacheKey, err = xcode.GetCacheKey(envProvider, xcode.CacheKeyParams{}); err != nil {
			return fmt.Errorf("get cache key: %w", err)
		}
		logger.TInfof("Downloading cache metadata checksum for key %s", cacheKey)
	} else {
		cacheKey = providedCacheKey
		logger.TInfof("Downloading cache metadata checksum for provided key %s", cacheKey)
	}

	var mdChecksum strings.Builder
	err = xcode.DownloadStreamFromBuildCache(&mdChecksum, cacheKey, endpointURL, authConfig, logger)
	if err != nil && !errors.Is(err, xcode.ErrCacheNotFound) {
		return fmt.Errorf("download cache metadata checksum: %w", err)
	}

	if errors.Is(err, xcode.ErrCacheNotFound) {
		fallbackCacheKey, fallbackErr := xcode.GetCacheKey(envProvider, xcode.CacheKeyParams{IsFallback: true})
		if fallbackErr != nil {
			return errors.New("cache metadata not found in cache")
		}

		cacheKey = fallbackCacheKey
		logger.Infof("Cache metadata not found for original key, trying fallback key %s", cacheKey)

		err = xcode.DownloadStreamFromBuildCache(&mdChecksum, cacheKey, endpointURL, authConfig, logger)
		if errors.Is(err, xcode.ErrCacheNotFound) {
			return errors.New("cache metadata not found in cache")
		}
		if err != nil {
			return fmt.Errorf("download cache metadata checksum: %w", err)
		}
		logger.Infof("Cache metadata found for fallback key %s", cacheKey)
	}

	logger.TInfof("Downloading cache metadata content to %s for key %s", cacheMetadataPath, mdChecksum.String())
	if err := xcode.DownloadFileFromBuildCache(cacheMetadataPath, mdChecksum.String(), endpointURL, authConfig, logger); err != nil {
		return fmt.Errorf("download cache archive: %w", err)
	}

	return nil
}

func logCacheMetadata(md *xcode.Metadata, logger log.Logger) {
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
	logger.Infof("  DerivedData files: %d", len(md.DerivedData.Files))
	logger.Infof("  Xcode cache files: %d", len(md.XcodeCacheDir.Files))
	logger.Infof("  Build Cache CLI version: %s", md.BuildCacheCLIVersion)
	logger.Infof("  Metadata version: %d", md.MetadataVersion)
}
