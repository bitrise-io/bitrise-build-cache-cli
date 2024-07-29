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
var deleteXcodeDerivedDataCmd = &cobra.Command{
	Use:   "delete-xcode-deriveddata",
	Short: "Deletes the DerivedData cache archive from the Bitrise Build Cache for a given key",
	Long:  `Deletes the DerivedData cache archive from the Bitrise Build Cache for a given key.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Delete the Xcode DerivedData archive from the Bitrise Build Cache")

		logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

		logger.Infof("(i) Checking parameters")
		cacheKey, _ := cmd.Flags().GetString("key")

		if err := deleteXcodeDerivedDataCmdFn(cacheKey, logger, os.Getenv); err != nil {
			return fmt.Errorf("delete Xcode DerivedData into Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ DerivedData cache archive deleted from Bitrise Build Cache ")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(deleteXcodeDerivedDataCmd)

	deleteXcodeDerivedDataCmd.Flags().String("key", "", "The cache key to be delete (set to the Bitrise app's slug and current git branch by default)")
}

func deleteXcodeDerivedDataCmdFn(cacheKey string, logger log.Logger, envProvider func(string) string) error {
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return fmt.Errorf("read auth config from environments: %w", err)
	}

	if cacheKey == "" {
		logger.Infof("(i) Cache key is not explicitly specified, setting it based on the current Bitrise app's slug and git branch...")
		if cacheKey, err = xcode.GetCacheKey(envProvider, xcode.CacheKeyParams{}); err != nil {
			return fmt.Errorf("get cache key: %w", err)
		}
	}
	logger.Infof("(i) Cache key: %s", cacheKey)

	endpointURL := common.SelectEndpointURL(envProvider("BITRISE_BUILD_CACHE_ENDPOINT"), envProvider)
	logger.Infof("(i) Build Cache Endpoint URL: %s", endpointURL)

	logger.TInfof("Creating empty cache archive")
	var cacheArchivePath string
	if cacheArchivePath, err = createEmptyCacheArchive(logger); err != nil {
		return fmt.Errorf("create empty cache archive: %w", err)
	}

	logger.TInfof("Uploading empty cache archive %s for key %s", cacheArchivePath, cacheKey)
	if err := xcode.UploadFileToBuildCache(cacheArchivePath, cacheKey, endpointURL, authConfig, logger); err != nil {
		return fmt.Errorf("upload cache archive: %w", err)
	}

	return nil
}

func createEmptyCacheArchive(logger log.Logger) (string, error) {
	emptyDir, err := os.MkdirTemp("", "empty-folder")
	if err != nil {
		return "", fmt.Errorf("create empty folder: %w", err)
	}
	defer os.RemoveAll(emptyDir)

	var emptyMetadata xcode.Metadata
	if err := xcode.SaveMetadata(&emptyMetadata, CacheMetadataPath, logger); err != nil {
		return "", fmt.Errorf("save metadata: %w", err)
	}

	cacheArchivePath := "bitrise-dd-cache/empty-dd.tar.zst"
	if err := xcode.CreateCacheArchive(cacheArchivePath, emptyDir, CacheMetadataPath, logger); err != nil {
		return "", fmt.Errorf("create cache archive: %w", err)
	}

	return cacheArchivePath, nil
}
