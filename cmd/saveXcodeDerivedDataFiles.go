package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	xa "github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/hash"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"github.com/bitrise-io/go-utils/v2/log"
)

const XCodeCacheMetadataPath = "dd-metadata.json"

// nolint: gochecknoglobals
var saveXcodeDerivedDataFilesCmd = &cobra.Command{
	Use:          "save-xcode-deriveddata-files",
	Short:        "Save the DerivedData folder into Bitrise Build Cache (file level)",
	Long:         `Save the contents of the DerivedData folder (used by Xcode to store intermediate build files) into Bitrise Build Cache.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logCurrentUserInfo(logger)

		logger.TInfof("Save Xcode DerivedData into Bitrise Build Cache")

		logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

		logger.Infof("(i) Checking parameters")
		projectRoot, _ := cmd.Flags().GetString("project-root")
		cacheKey, _ := cmd.Flags().GetString("key")
		ddPath, _ := cmd.Flags().GetString("deriveddata-path")
		xcodeCachePath, _ := cmd.Flags().GetString("xcodecache-path")
		followSymlinks, _ := cmd.Flags().GetBool("follow-symlinks")
		skipSPM, _ := cmd.Flags().GetBool("skip-spm")

		tracker := xcode.NewDefaultStepTracker("save-xcode-build-cache", os.Getenv, logger)
		defer tracker.Wait()
		startT := time.Now()

		logger.Infof("(i) Check Auth Config")
		authConfig, err := common.ReadAuthConfigFromEnvironments(os.Getenv)
		if err != nil {
			return fmt.Errorf("read auth config from environments: %w", err)
		}

		op, cmdError := saveXcodeDerivedDataFilesCmdFn(cmd.Context(),
			authConfig,
			XCodeCacheMetadataPath,
			projectRoot,
			cacheKey,
			ddPath,
			xcodeCachePath,
			followSymlinks,
			skipSPM,
			logger,
			tracker,
			startT,
			os.Getenv)
		if op != nil {
			if cmdError != nil {
				errStr := cmdError.Error()
				op.Error = &errStr
			}

			if err := sendCacheOperationAnalytics(*op, logger, authConfig); err != nil {
				logger.Warnf("Failed to send cache operation analytics: %s", err)
			}
		}

		tracker.LogSaveFinished(time.Since(startT), cmdError)
		if cmdError != nil {
			return fmt.Errorf("save Xcode cache into Bitrise Build Cache: %w", cmdError)
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
	saveXcodeDerivedDataFilesCmd.Flags().Bool("follow-symlinks", false, "Follow symlinks when calculating metadata and save referenced files to the cache (default: false)")
	saveXcodeDerivedDataFilesCmd.Flags().Bool("skip-spm", false, "Skip saving files under \"DerivedData/*/SourcePackages\", i.e. skip SPM dependencies. Consider enabling this flag if using SPM cache steps. Default: false")
}

func saveXcodeDerivedDataFilesCmdFn(ctx context.Context,
	authConfig common.CacheAuthConfig,
	cacheMetadataPath,
	projectRoot,
	providedCacheKey,
	derivedDataPath,
	xcodeCachePath string,
	followSymlinks bool,
	skipSPM bool,
	logger log.Logger,
	tracker xcode.StepAnalyticsTracker,
	startT time.Time,
	envProvider func(string) string) (*xa.CacheOperation, error) {
	var err error
	var cacheKey string
	if providedCacheKey == "" {
		logger.Infof("(i) Cache key is not explicitly specified, setting it based on the current Bitrise app's slug and git branch...")
		if cacheKey, err = xcode.GetCacheKey(envProvider, xcode.CacheKeyParams{}); err != nil {
			return nil, fmt.Errorf("get cache key: %w", err)
		}
	} else {
		cacheKey = providedCacheKey
	}
	logger.Infof("(i) Cache key: %s", cacheKey)

	op := newCacheOperation(startT, xa.OperationTypeUpload, envProvider)
	op.CacheKey = cacheKey
	logger.Infof("(i) Cache operation ID: %s", op.OperationID)

	kvClient, err := createKVClient(ctx, op.OperationID, authConfig, envProvider, logger)
	if err != nil {
		return op, fmt.Errorf("create kv client: %w", err)
	}

	absoluteRootDir, err := filepath.Abs(projectRoot)
	if err != nil {
		return op, fmt.Errorf("get absolute path of rootDir: %w", err)
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
		FollowSymlinks:     followSymlinks,
		SkipSPM:            skipSPM,
	}, envProvider, logger)
	if err != nil {
		return op, fmt.Errorf("create metadata: %w", err)
	}

	logger.TInfof("Saving metadata file %s", cacheMetadataPath)
	metadataSize, err := xcode.SaveMetadata(metadata, cacheMetadataPath, logger)
	if err != nil {
		return op, fmt.Errorf("save metadata: %w", err)
	}

	metadataSavedT := time.Now()
	tracker.LogMetadataSaved(metadataSavedT.Sub(startT), len(metadata.ProjectFiles.Files)+len(metadata.ProjectFiles.Directories), metadataSize)

	mdChecksum, err := hash.ChecksumOfFile(cacheMetadataPath)
	mdChecksumReader := strings.NewReader(mdChecksum)
	if err != nil {
		return op, fmt.Errorf("checksum of metadata file: %w", err)
	}

	logger.TInfof("Uploading DerivedData files")
	ddUploadStats, err := kvClient.UploadFileGroupToBuildCache(ctx, metadata.DerivedData)
	ddUploadedT := time.Now()
	fillCacheOperationWithUploadStats(op, ddUploadStats)
	tracker.LogDerivedDataUploaded(ddUploadedT.Sub(metadataSavedT), ddUploadStats)

	if err != nil {
		return op, fmt.Errorf("upload derived data files to build cache: %w", err)
	}

	if xcodeCachePath != "" {
		logger.TInfof("Uploading Xcode cache files")
		if _, err := kvClient.UploadFileGroupToBuildCache(ctx, metadata.XcodeCacheDir); err != nil {
			return op, fmt.Errorf("upload xcode cache files to build cache: %w", err)
		}
	}

	logger.TInfof("Uploading metadata checksum of %s (%s) for key %s", cacheMetadataPath, mdChecksum, cacheKey)
	if err := kvClient.UploadStreamToBuildCache(ctx, mdChecksumReader, cacheKey, mdChecksumReader.Size()); err != nil {
		return op, fmt.Errorf("upload metadata checksum to build cache: %w", err)
	}

	logger.TInfof("Uploading metadata content of %s for key %s", cacheMetadataPath, mdChecksum)
	if err := kvClient.UploadFileToBuildCache(ctx, cacheMetadataPath, mdChecksum); err != nil {
		return op, fmt.Errorf("upload metadata content to build cache: %w", err)
	}

	if providedCacheKey == "" {
		fallbackCacheKey, err := xcode.GetCacheKey(envProvider, xcode.CacheKeyParams{IsFallback: true})
		if err != nil {
			logger.Warnf("Failed to get fallback cache key: %s", err)
		} else if fallbackCacheKey != "" && cacheKey != fallbackCacheKey {
			cacheKey = fallbackCacheKey
			mdChecksumReader = strings.NewReader(mdChecksum) // reset reader
			logger.TInfof("Uploading metadata checksum of %s (%s) for fallback key %s", cacheMetadataPath, mdChecksum, cacheKey)
			if err := kvClient.UploadStreamToBuildCache(ctx, mdChecksumReader, cacheKey, mdChecksumReader.Size()); err != nil {
				return op, fmt.Errorf("upload metadata checksum to build cache: %w", err)
			}
		}
	}

	return op, nil
}
