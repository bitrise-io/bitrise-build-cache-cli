package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"github.com/bitrise-io/go-utils/v2/log"
)

// nolint: gochecknoglobals
var restoreXcodeDerivedDataCmd = &cobra.Command{
	Use:   "restore-xcode-deriveddata",
	Short: "Restore the DerivedData folder from Bitrise Build Cache",
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

		if err := restoreXcodeDerivedDataCmdFn(cacheArchivePath, CacheMetadataPath, projectRoot, cacheKey, logger, os.Getenv); err != nil {
			return fmt.Errorf("restore Xcode DerivedData from Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ DerivedData restored from Bitrise Build Cache ")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(restoreXcodeDerivedDataCmd)

	restoreXcodeDerivedDataCmd.Flags().String("key", "", "The cache key to use for the saved cache item (set to the Bitrise app's slug and current git branch by default)")
	restoreXcodeDerivedDataCmd.Flags().String("cache-archive", "bitrise-dd-cache/dd.tar.zst", "Path to the uploadable cache archive with the contents of the DerivedData folder")
	restoreXcodeDerivedDataCmd.Flags().String("project-root", "", "Path to the iOS project folder to be built (this is used when restoring the modification time of the source files)")
	if err := restoreXcodeDerivedDataCmd.MarkFlagRequired("project-root"); err != nil {
		panic(err)
	}
}

func restoreXcodeDerivedDataCmdFn(cacheArchivePath, cacheMetadataPath, projectRoot, cacheKey string, logger log.Logger, envProvider func(string) string) error {
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return fmt.Errorf("read auth config from environments: %w", err)
	}

	if cacheKey == "" {
		if cacheKey, err = xcode.GetCacheKey(envProvider); err != nil {
			return fmt.Errorf("get cache key: %w", err)
		}
	}
	logger.Infof("(i) Cache key: %s", cacheKey)

	endpointURL := common.SelectEndpointURL(envProvider("BITRISE_BUILD_CACHE_ENDPOINT"), envProvider)
	logger.Infof("(i) Build Cache Endpoint URL: %s", endpointURL)

	logger.TInfof("Downloading cache archive for key %s", cacheKey)
	if err := xcode.DownloadFromBuildCache(cacheArchivePath, cacheKey, endpointURL, authConfig, logger); err != nil {
		return fmt.Errorf("download cache archive: %w", err)
	}

	logger.TInfof("Extracting cache archive")
	if err := xcode.ExtractCacheArchive(cacheArchivePath, logger); err != nil {
		return fmt.Errorf("extract cache archive: %w", err)
	}

	logger.TInfof("Loading metadata of the cache archive from %s", cacheMetadataPath)
	var metadata *xcode.Metadata
	if metadata, err = xcode.LoadMetadata(cacheMetadataPath); err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}

	logCacheMetadata(metadata, logger)

	logger.TInfof("Restoring modification time of input files")
	if err := xcode.RestoreMTime(metadata, projectRoot, logger); err != nil {
		return fmt.Errorf("restore modification time: %w", err)
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
}
