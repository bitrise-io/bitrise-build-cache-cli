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
		cacheArchivePath, _ := cmd.Flags().GetString("cache-archive")
		projectRoot, _ := cmd.Flags().GetString("project-root")
		cacheKey, _ := cmd.Flags().GetString("key")

		if err := restoreXcodeDerivedDataFilesCmdFn(cacheArchivePath, CacheMetadataPath, projectRoot, cacheKey, logger, os.Getenv); err != nil {
			return fmt.Errorf("restore Xcode DerivedData from Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ DerivedData restored from Bitrise Build Cache ")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(restoreXcodeDerivedDataFilesCmd)

	restoreXcodeDerivedDataFilesCmd.Flags().String("key", "", "The cache key to use for the saved cache item (set to the Bitrise app's slug and current git branch by default)")
	restoreXcodeDerivedDataFilesCmd.Flags().String("cache-archive", "bitrise-dd-cache/dd.tar.zst", "Path to the uploadable cache archive with the contents of the DerivedData folder")
	restoreXcodeDerivedDataFilesCmd.Flags().String("project-root", "", "Path to the iOS project folder to be built (this is used when restoring the modification time of the source files)")
	if err := restoreXcodeDerivedDataFilesCmd.MarkFlagRequired("project-root"); err != nil {
		panic(err)
	}
}

func restoreXcodeDerivedDataFilesCmdFn(cacheArchivePath, cacheMetadataPath, projectRoot, cacheKey string, logger log.Logger, envProvider func(string) string) error {
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

	//archiveDownloadedT := time.Now()
	//var archiveSize int64
	//if archiveSize, err = getFileSize(cacheArchivePath); err != nil {
	//	return fmt.Errorf("get file size: %w", err)
	//}
	//tracker.LogArchiveDownloaded(archiveDownloadedT.Sub(startT), archiveSize)
	//
	//logger.TInfof("Extracting cache archive")
	//if err := xcode.ExtractCacheArchive(cacheArchivePath, logger); err != nil {
	//	return fmt.Errorf("extract cache archive: %w", err)
	//}
	//
	//archiveExtractedT := time.Now()
	//tracker.LogArchiveExtracted(archiveExtractedT.Sub(archiveDownloadedT), archiveSize)

	logger.TInfof("Loading metadata of the cache archive from %s", cacheMetadataPath)
	var metadata *xcode.Metadata
	if metadata, err = xcode.LoadMetadata(cacheMetadataPath); err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	logCacheMetadata(metadata, logger)

	logger.TInfof("Restoring modification time of input files")
	//var filesUpdated int
	if _, err = xcode.RestoreMTime(metadata, projectRoot, logger); err != nil {
		return fmt.Errorf("restore modification time: %w", err)
	}

	logger.TInfof("Downloading DerivedData files")
	if err := xcode.DownloadDerivedDataFilesFromBuildCache(metadata.DerivedData, endpointURL, authConfig, logger); err != nil {
		return fmt.Errorf("download DerivedData files: %w", err)
	}

	//metadataLoadedT := time.Now()
	//tracker.LogMetadataLoaded(metadataLoadedT.Sub(archiveExtractedT), metadataLoadedT.Sub(startT), len(metadata.InputFiles), filesUpdated)

	return nil
}
