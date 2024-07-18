package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"github.com/bitrise-io/go-utils/v2/log"
)

// nolint: gochecknoglobals
var saveXcodeDerivedDataFilesCmd = &cobra.Command{
	Use:   "save-xcode-deriveddata-files",
	Short: "Save the DerivedData folder into Bitrise Build Cache (file level)",
	Long:  `Save the contents of the DerivedData folder (used by Xcode to store intermediate build files) into Bitrise Build Cache.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Save Xcode DerivedData into Bitrise Build Cache")

		logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

		logger.Infof("(i) Checking parameters")
		cacheArchivePath, _ := cmd.Flags().GetString("cache-archive")
		projectRoot, _ := cmd.Flags().GetString("project-root")
		cacheKey, _ := cmd.Flags().GetString("key")
		ddPath, _ := cmd.Flags().GetString("deriveddata-path")

		if err := saveXcodeDerivedDataFilesCmdFn(cacheArchivePath, CacheMetadataPath, projectRoot, cacheKey, ddPath, logger, os.Getenv); err != nil {
			return fmt.Errorf("save Xcode DerivedData into Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ DerivedData saved into Bitrise Build Cache ")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(saveXcodeDerivedDataFilesCmd)

	saveXcodeDerivedDataFilesCmd.Flags().String("key", "", "The cache key to use for the saved cache item (set to the Bitrise app's slug and current git branch by default)")
	saveXcodeDerivedDataFilesCmd.Flags().String("cache-archive", "bitrise-dd-cache/dd.tar.zst", "Path to the uploadable cache archive with the contents of the DerivedData folder")
	saveXcodeDerivedDataFilesCmd.Flags().String("project-root", "", "Path to the iOS project folder to be built (this is used when saving the modification time of the source files)")
	if err := saveXcodeDerivedDataFilesCmd.MarkFlagRequired("project-root"); err != nil {
		panic(err)
	}
	saveXcodeDerivedDataFilesCmd.Flags().String("deriveddata-path", "", "Path to the DerivedData folder used by the build - "+
		"NOTE: this must be the same folder specified for the -derivedDataPath flag when running xcodebuild e.g. xcodebuild -derivedData \"~/DerivedData/MyProject\"")
	if err := saveXcodeDerivedDataCmd.MarkFlagRequired("deriveddata-path"); err != nil {
		panic(err)
	}
}

// nolint:cyclop
func saveXcodeDerivedDataFilesCmdFn(cacheArchivePath, cacheMetadataPath, projectRoot, cacheKey, derivedDataPath string, logger log.Logger, envProvider func(string) string) error {
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return fmt.Errorf("read auth config from environments: %w", err)
	}

	if cacheKey == "" {
		logger.Infof("(i) Cache key is not explicitly specified, setting it based on the current Bitrise app's slug and git branch...")
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

	tracker := xcode.NewStepTracker("save-xcode-build-cache", envProvider, logger)
	defer tracker.Wait()
	startT := time.Now()

	absoluteRootDir, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("get absolute path of rootDir: %w", err)
	}
	logger.TInfof("Gathering metadata for input files in %s and DerivedData in %s", absoluteRootDir, derivedDataPath)
	metadata, err := xcode.CreateMetadata(xcode.CreateMetadataParams{
		ProjectRootDirPath: absoluteRootDir,
		DerivedDataPath:    derivedDataPath,
		CacheKey:           cacheKey,
	}, envProvider, logger)
	if err != nil {
		return fmt.Errorf("create metadata: %w", err)
	}

	logger.TInfof("Saving metadata file %s", cacheMetadataPath)
	if err := xcode.SaveMetadata(metadata, cacheMetadataPath, logger); err != nil {
		return fmt.Errorf("save metadata: %w", err)
	}

	metadataSavedT := time.Now()
	tracker.LogMetadataSaved(metadataSavedT.Sub(startT), len(metadata.InputFiles))

	logger.TInfof("Uploading metadata %s for key %s", cacheMetadataPath, cacheKey)
	if err := xcode.UploadToBuildCache(cacheMetadataPath, cacheKey, endpointURL, authConfig, logger); err != nil {
		return fmt.Errorf("upload cache archive: %w", err)
	}

	logger.TInfof("Uploading DerivedData files")
	if err := xcode.UploadDerivedDataFilesToBuildCache(metadata.DerivedData, endpointURL, authConfig, logger); err != nil {
		return fmt.Errorf("upload derived data files to build cache: %w", err)
	}

	return nil
}