package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"github.com/bitrise-io/go-utils/v2/log"
	"path/filepath"
	"strings"
	"time"
)

const CacheMetadataPath = "dd-metadata.json"

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
		projectRoot, _ := cmd.Flags().GetString("project-root")
		cacheKey, _ := cmd.Flags().GetString("key")
		ddPath, _ := cmd.Flags().GetString("deriveddata-path")
		xcodeCachePath, _ := cmd.Flags().GetString("xcodecache-path")

		if err := saveXcodeDerivedDataFilesCmdFn(CacheMetadataPath, projectRoot, cacheKey, ddPath, xcodeCachePath, logger, os.Getenv); err != nil {
			return fmt.Errorf("save Xcode cache into Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ Cache directories saved into Bitrise Build Cache ")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(saveXcodeDerivedDataFilesCmd)

	saveXcodeDerivedDataFilesCmd.Flags().String("key", "", "The cache key to use for the saved cache item (set to the Bitrise app's slug and current git branch by default)")
	saveXcodeDerivedDataFilesCmd.Flags().String("project-root", "", "Path to the iOS project folder to be built (this is used when saving the modification time of the source files)")
	if err := saveXcodeDerivedDataFilesCmd.MarkFlagRequired("project-root"); err != nil {
		panic(err)
	}
	saveXcodeDerivedDataFilesCmd.Flags().String("deriveddata-path", "", "Path to the DerivedData folder used by the build - "+
		"NOTE: this must be the same folder specified for the -derivedDataPath flag when running xcodebuild e.g. xcodebuild -derivedData \"~/FileGroupMetadata/MyProject\"")
	if err := saveXcodeDerivedDataFilesCmd.MarkFlagRequired("deriveddata-path"); err != nil {
		panic(err)
	}
	saveXcodeDerivedDataFilesCmd.Flags().String("xcodecache-path", "", "Path to the Xcode cache directory folder to be saved. If not set, it will not be uploaded.")
}

func saveXcodeDerivedDataFilesCmdFn(cacheMetadataPath, projectRoot, providedCacheKey, derivedDataPath, xcodeCachePath string, logger log.Logger, envProvider func(string) string) error {
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return fmt.Errorf("read auth config from environments: %w", err)
	}

	var cacheKey string
	if providedCacheKey == "" {
		logger.Infof("(i) Cache key is not explicitly specified, setting it based on the current Bitrise app's slug and git branch...")
		if cacheKey, err = xcode.GetCacheKey(envProvider, xcode.CacheKeyParams{}); err != nil {
			return fmt.Errorf("get cache key: %w", err)
		}
	} else {
		cacheKey = providedCacheKey
	}
	logger.Infof("(i) Cache key: %s", cacheKey)

	kvClient, err := createKVClient(authConfig, envProvider, logger)
	if err != nil {
		return fmt.Errorf("create kv client: %w", err)
	}

	tracker := xcode.NewStepTracker("save-xcode-build-cache", envProvider, logger)
	defer tracker.Wait()
	startT := time.Now()

	absoluteRootDir, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("get absolute path of rootDir: %w", err)
	}

	metadataSaveMsg := fmt.Sprintf("Gathering metadata for input files in %s, DerivedData in %s", absoluteRootDir, derivedDataPath)
	if xcodeCachePath != "" {
		metadataSaveMsg += fmt.Sprintf(", Xcode cache directory in %s", xcodeCachePath)
	}
	logger.TInfof(metadataSaveMsg)
	metadata, err := xcode.CreateMetadata(xcode.CreateMetadataParams{
		ProjectRootDirPath: absoluteRootDir,
		DerivedDataPath:    derivedDataPath,
		XcodeCacheDirPath:  xcodeCachePath,
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
	tracker.LogMetadataSaved(metadataSavedT.Sub(startT), len(metadata.ProjectFiles.Files))

	mdChecksum, err := xcode.ChecksumOfFile(cacheMetadataPath)
	mdChecksumReader := strings.NewReader(mdChecksum)
	if err != nil {
		return fmt.Errorf("checksum of metadata file: %w", err)
	}

	logger.TInfof("Uploading metadata checksum of %s (%s) for key %s", cacheMetadataPath, mdChecksum, cacheKey)
	if err := xcode.UploadStreamToBuildCache(mdChecksumReader, cacheKey, mdChecksumReader.Size(), kvClient, logger); err != nil {
		return fmt.Errorf("upload metadata checksum to build cache: %w", err)
	}

	logger.TInfof("Uploading metadata content of %s for key %s", cacheMetadataPath, mdChecksum)
	if err := xcode.UploadFileToBuildCache(cacheMetadataPath, mdChecksum, kvClient, logger); err != nil {
		return fmt.Errorf("upload metadata content to build cache: %w", err)
	}

	if providedCacheKey == "" {
		fallbackCacheKey, err := xcode.GetCacheKey(envProvider, xcode.CacheKeyParams{IsFallback: true})
		if err != nil {
			logger.Warnf("Failed to get fallback cache key: %s", err)
		} else if fallbackCacheKey != "" && cacheKey != fallbackCacheKey {
			cacheKey = fallbackCacheKey
			mdChecksumReader = strings.NewReader(mdChecksum) // reset reader
			logger.TInfof("Uploading metadata checksum of %s (%s) for fallback key %s", cacheMetadataPath, mdChecksum, cacheKey)
			if err := xcode.UploadStreamToBuildCache(mdChecksumReader, cacheKey, mdChecksumReader.Size(), kvClient, logger); err != nil {
				return fmt.Errorf("upload metadata checksum to build cache: %w", err)
			}
		}
	}

	logger.TInfof("Uploading DerivedData files")
	if err := xcode.UploadCacheFilesToBuildCache(metadata.DerivedData, kvClient, logger); err != nil {
		return fmt.Errorf("upload derived data files to build cache: %w", err)
	}

	if xcodeCachePath != "" {
		logger.TInfof("Uploading Xcode cache files")
		if err := xcode.UploadCacheFilesToBuildCache(metadata.XcodeCacheDir, kvClient, logger); err != nil {
			return fmt.Errorf("upload xcode cache files to build cache: %w", err)
		}
	}

	return nil
}
