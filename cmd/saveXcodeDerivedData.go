package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"github.com/bitrise-io/go-utils/v2/log"
)

var cacheArchivePath string
var projectRoot string
var derivedDataPath string
var cacheMetadataPath string = "dd-metadata.json"
var cacheKey string

var saveXcodeDerivedDataCmd = &cobra.Command{
	Use:   "save-xcode-deriveddata",
	Short: "Save the DerivedData folder into Bitrise Build Cache",
	Long:  `Save the contents of the DerivedData folder (used by Xcode to store intermediate build files) into Bitrise Build Cache.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Save Xcode DerivedData into Bitrise Build Cache")

		if err := saveXcodeDerivedDataCmdFn(logger, os.Getenv); err != nil {
			return fmt.Errorf("save Xcode DerivedData into Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ DerivedData saved into Bitrise Build Cache ")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(saveXcodeDerivedDataCmd)

	saveXcodeDerivedDataCmd.Flags().StringVarP(&cacheKey, "key", "", "", "The cache key to use for the saved cache item (set to the current git branch by default")
	saveXcodeDerivedDataCmd.Flags().StringVarP(&cacheArchivePath, "cache-archive", "", "bitrise-dd-cache/dd.tar.zst", "Path to the uploadable cache archive with the contents of the DerivedData folder")
	saveXcodeDerivedDataCmd.Flags().StringVarP(&projectRoot, "project-root", "", "", "Path to the iOS project folder to be built (this is used when saving the modification time of the source files)")
	saveXcodeDerivedDataCmd.MarkFlagRequired("project-root")
	saveXcodeDerivedDataCmd.Flags().StringVarP(&derivedDataPath, "deriveddata-path", "", "", "Path to the DerivedData folder used by the build - "+
		"NOTE: this must be the same folder specified for the -derivedDataPath flag when running xcodebuild e.g. xcodebuild -derivedData \"~/DerivedData/MyProject\"")
}

func saveXcodeDerivedDataCmdFn(logger log.Logger, envProvider func(string) string) error {
	logger.Infof("(i) Checking parameters")

	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return fmt.Errorf("read auth config from environments: %w", err)
	}

	if cacheKey == "" {
		branch := envProvider("BITRISE_GIT_BRANCH")
		if branch == "" {
			return fmt.Errorf("cache key is required if BITRISE_GIT_BRANCH is not set")
		}

		cacheKey = fmt.Sprintf("xcode-deriveddata-%s", branch)
	}
	logger.Infof("(i) Cache key prefix: %s", cacheKey)

	endpointURL := common.SelectEndpointURL(envProvider("BITRISE_BUILD_CACHE_ENDPOINT"), envProvider)
	logger.Infof("(i) Build Cache Endpoint URL: %s", endpointURL)

	if err := xcode.SaveMetadata(projectRoot, cacheMetadataPath, logger); err != nil {
		return fmt.Errorf("save metadata: %w", err)
	}

	if err := xcode.UploadToBuildCache(cacheArchivePath, fmt.Sprintf("%s-archive", cacheKey), authConfig.AuthToken, endpointURL, logger); err != nil {
		return fmt.Errorf("upload cache archive: %w", err)
	}

	if err := xcode.UploadToBuildCache(cacheMetadataPath, fmt.Sprintf("%s-metadata", cacheKey), authConfig.AuthToken, endpointURL, logger); err != nil {
		return fmt.Errorf("upload cache metadata: %w", err)
	}

	return nil
}
