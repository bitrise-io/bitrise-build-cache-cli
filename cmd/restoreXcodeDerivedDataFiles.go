package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"github.com/bitrise-io/go-utils/v2/log"
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

func restoreXcodeDerivedDataFilesCmdFn(cacheMetadataPath, projectRoot, cacheKey string, logger log.Logger, envProvider func(string) string) error {
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return fmt.Errorf("read auth config from environments: %w", err)
	}

	if cacheKey == "" {
		if cacheKey, err = xcode.GetCacheKey(envProvider, true); err != nil {
			return fmt.Errorf("get cache key: %w", err)
		}
	}
	logger.Infof("(i) Cache key: %s", cacheKey)

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
	//startT := time.Now()

	logger.TInfof("Downloading cache metadata for key %s", cacheKey)
	if err := xcode.DownloadFromBuildCache(cacheMetadataPath, cacheKey, endpointURL, authConfig, logger); err != nil {
		return fmt.Errorf("download cache archive: %w", err)
	}

	logger.TInfof("Loading metadata of the cache archive from %s", cacheMetadataPath)
	var metadata *xcode.Metadata
	if metadata, err = xcode.LoadMetadata(cacheMetadataPath); err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	logCacheMetadata(metadata, logger)

	logger.TInfof("Restoring metadata of input files")
	//var filesUpdated int
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
