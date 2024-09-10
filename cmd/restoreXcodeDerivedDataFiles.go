package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"sort"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	xa "github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
)

const (
	CacheKeyTypeDefault  = "default"
	CacheKeyTypeFallback = "fallback"
	CacheKeyTypeProvided = "provided"
)

type CacheKeyType string

// nolint: gochecknoglobals
var restoreXcodeDerivedDataFilesCmd = &cobra.Command{
	Use:          "restore-xcode-deriveddata-files",
	Short:        "Restore the DerivedData folder from Bitrise Build Cache (per file)",
	Long:         `Restore the contents of the DerivedData folder (used by Xcode to store intermediate build files) from Bitrise Build Cache.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logCurrentUserInfo(logger)

		logger.TInfof("Restore Xcode DerivedData from Bitrise Build Cache")

		logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

		logger.Infof("(i) Checking parameters")
		projectRoot, _ := cmd.Flags().GetString("project-root")
		cacheKey, _ := cmd.Flags().GetString("key")
		forceOverwrite, _ := cmd.Flags().GetBool("force-overwrite-files")
		maxLoggedErrors, _ := cmd.Flags().GetInt("max-logged-errors")

		tracker := xcode.NewDefaultStepTracker("restore-xcode-build-cache", os.Getenv, logger)
		defer tracker.Wait()
		startT := time.Now()

		logger.Infof("(i) Check Auth Config")
		authConfig, err := common.ReadAuthConfigFromEnvironments(os.Getenv)
		if err != nil {
			return fmt.Errorf("read auth config from environments: %w", err)
		}

		op, cmdError := restoreXcodeDerivedDataFilesCmdFn(cmd.Context(), authConfig, CacheMetadataPath, projectRoot,
			cacheKey, logger, tracker, startT, os.Getenv, isDebugLogMode, forceOverwrite, maxLoggedErrors)
		if op != nil {
			if cmdError != nil {
				errStr := cmdError.Error()
				op.Error = &errStr
			}

			if err := sendCacheOperationAnalytics(*op, logger, authConfig); err != nil {
				logger.Warnf("Failed to send cache operation analytics: %s", err)
			}
		}

		tracker.LogRestoreFinished(time.Since(startT), err)
		if cmdError != nil {
			return fmt.Errorf("restore Xcode DerivedData from Bitrise Build Cache: %w", cmdError)
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
	restoreXcodeDerivedDataFilesCmd.Flags().Bool("force-overwrite-files", false, "If set, the command will try to overwrite existing files during restoring the cache even if the permissions do not allow it")
	restoreXcodeDerivedDataFilesCmd.Flags().Int("max-logged-errors", 150, "The maximum number of errors logged to the console during restoring the cache.")
}

func restoreXcodeDerivedDataFilesCmdFn(ctx context.Context, authConfig common.CacheAuthConfig, cacheMetadataPath, projectRoot, providedCacheKey string, logger log.Logger,
	tracker xcode.StepAnalyticsTracker, startT time.Time, envProvider func(string) string, isDebugLogMode, forceOverwrite bool, maxLoggedDownloadErrors int) (*xa.CacheOperation, error) {
	op := newCacheOperation(startT, xa.OperationTypeDownload, envProvider)
	kvClient, err := createKVClient(ctx, op.OperationID, authConfig, envProvider, logger)
	if err != nil {
		return nil, fmt.Errorf("create kv client: %w", err)
	}
	logger.Infof("(i) Cache operation ID: %s", op.OperationID)

	cacheKeyType, cacheKey, err := downloadMetadata(ctx, cacheMetadataPath, providedCacheKey, kvClient, logger, envProvider)
	op.CacheKey = cacheKey
	if err != nil {
		return op, fmt.Errorf("download cache metadata: %w", err)
	}

	cacheKeyTypeStr := string(cacheKeyType)
	op.CacheKeyType = &cacheKeyTypeStr

	logger.TInfof("Loading metadata of the cache archive from %s", cacheMetadataPath)
	var metadata *xcode.Metadata
	var metadataSize int64
	if metadata, metadataSize, err = xcode.LoadMetadata(cacheMetadataPath); err != nil {
		return op, fmt.Errorf("load metadata: %w", err)
	}

	logCacheMetadata(metadata, logger, isDebugLogMode)

	logger.TInfof("Restoring metadata of input files")
	var filesUpdated int
	if filesUpdated, err = xcode.RestoreFileInfos(metadata.ProjectFiles.Files, projectRoot, logger); err != nil {
		return op, fmt.Errorf("restore modification time: %w", err)
	}

	logger.TInfof("Restoring metadata of input directories")
	if err := xcode.RestoreDirectoryInfos(metadata.ProjectFiles.Directories, projectRoot, logger); err != nil {
		return op, fmt.Errorf("restore metadata of input directories: %w", err)
	}

	metadataRestoredT := time.Now()
	tracker.LogMetadataLoaded(metadataRestoredT.Sub(startT), string(cacheKeyType), len(metadata.ProjectFiles.Files)+len(metadata.ProjectFiles.Directories), filesUpdated, metadataSize)

	logger.TInfof("Downloading DerivedData files")
	stats, err := xcode.DownloadCacheFilesFromBuildCache(ctx, metadata.DerivedData, kvClient, logger, isDebugLogMode, forceOverwrite, maxLoggedDownloadErrors)
	ddDownloadedT := time.Now()
	tracker.LogDerivedDataDownloaded(ddDownloadedT.Sub(metadataRestoredT), stats)
	fillCacheOperationWithDownloadStats(op, stats)
	if err != nil {
		logger.Infof("Failed to download DerivedData files, clearing")
		// To prevent the build from failing
		xcode.DeleteFileGroup(metadata.DerivedData, logger)

		return op, fmt.Errorf("download DerivedData files: %w", err)
	}

	logger.TInfof("Restoring DerivedData directory metadata")
	if err := xcode.RestoreDirectoryInfos(metadata.DerivedData.Directories, "", logger); err != nil {
		return op, fmt.Errorf("restore DerivedData directories: %w", err)
	}

	if len(metadata.ProjectFiles.Symlinks) > 0 {
		logger.TInfof("Restoring project symlinks")
		if _, err = xcode.RestoreSymlinks(metadata.ProjectFiles.Symlinks, logger); err != nil {
			return op, fmt.Errorf("restore project symlink: %w", err)
		}
	}

	if len(metadata.DerivedData.Symlinks) > 0 {
		logger.TInfof("Restoring DerivedData symlinks")
		if _, err = xcode.RestoreSymlinks(metadata.DerivedData.Symlinks, logger); err != nil {
			return op, fmt.Errorf("restore DerivedData symlink: %w", err)
		}
	}

	if len(metadata.XcodeCacheDir.Files) > 0 {
		logger.TInfof("Downloading Xcode cache files")
		if _, err := xcode.DownloadCacheFilesFromBuildCache(ctx, metadata.XcodeCacheDir, kvClient, logger, isDebugLogMode, forceOverwrite, maxLoggedDownloadErrors); err != nil {
			return op, fmt.Errorf("download Xcode cache files: %w", err)
		}

		logger.TInfof("Restoring Xcode cache directory metadata")
		if err := xcode.RestoreDirectoryInfos(metadata.XcodeCacheDir.Directories, "", logger); err != nil {
			return op, fmt.Errorf("restore Xcode cache directories: %w", err)
		}
	}

	if len(metadata.XcodeCacheDir.Symlinks) > 0 {
		logger.TInfof("Restoring Xcode cache symlinks")
		if _, err = xcode.RestoreSymlinks(metadata.XcodeCacheDir.Symlinks, logger); err != nil {
			return op, fmt.Errorf("restore xcode symlink: %w", err)
		}
	}

	return op, nil
}

func downloadMetadata(ctx context.Context, cacheMetadataPath, providedCacheKey string,
	kvClient *kv.Client,
	logger log.Logger,
	envProvider func(string) string) (CacheKeyType, string, error) {
	var cacheKeyType CacheKeyType = CacheKeyTypeDefault
	var cacheKey string
	var err error
	if providedCacheKey == "" {
		if cacheKey, err = xcode.GetCacheKey(envProvider, xcode.CacheKeyParams{}); err != nil {
			return "", "", fmt.Errorf("get cache key: %w", err)
		}
		logger.TInfof("Downloading cache metadata checksum for key %s", cacheKey)
	} else {
		cacheKeyType = CacheKeyTypeProvided
		cacheKey = providedCacheKey
		logger.TInfof("Downloading cache metadata checksum for provided key %s", cacheKey)
	}

	var mdChecksum strings.Builder
	err = xcode.DownloadStreamFromBuildCache(ctx, &mdChecksum, cacheKey, kvClient, logger)
	if err != nil && !errors.Is(err, xcode.ErrCacheNotFound) {
		return cacheKeyType, cacheKey, fmt.Errorf("download cache metadata checksum: %w", err)
	}

	if errors.Is(err, xcode.ErrCacheNotFound) {
		cacheKeyType = CacheKeyTypeFallback
		fallbackCacheKey, fallbackErr := xcode.GetCacheKey(envProvider, xcode.CacheKeyParams{IsFallback: true})
		if fallbackErr != nil {
			return cacheKeyType, fallbackCacheKey, errors.New("cache metadata not found in cache")
		}

		cacheKey = fallbackCacheKey
		logger.Infof("Cache metadata not found for original key, trying fallback key %s", cacheKey)

		err = xcode.DownloadStreamFromBuildCache(ctx, &mdChecksum, cacheKey, kvClient, logger)
		if errors.Is(err, xcode.ErrCacheNotFound) {
			return cacheKeyType, cacheKey, errors.New("cache metadata not found in cache")
		}
		if err != nil {
			return cacheKeyType, cacheKey, fmt.Errorf("download cache metadata checksum: %w", err)
		}
		logger.Infof("Cache metadata found for fallback key %s", cacheKey)
	}

	logger.TInfof("Downloading cache metadata content to %s for key %s", cacheMetadataPath, mdChecksum.String())
	if err := xcode.DownloadFileFromBuildCache(ctx, cacheMetadataPath, mdChecksum.String(), kvClient, logger); err != nil {
		return cacheKeyType, cacheKey, fmt.Errorf("download cache archive: %w", err)
	}

	return cacheKeyType, cacheKey, nil
}

func logCacheMetadata(md *xcode.Metadata, logger log.Logger, isDebugLogMode bool) {
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
	logger.Infof("  Project sylinks: %d", len(md.ProjectFiles.Symlinks))
	logger.Infof("  DerivedData files: %d", len(md.DerivedData.Files))
	logger.Infof("  DerivedData symlinks: %d", len(md.DerivedData.Symlinks))
	logger.Infof("  Xcode cache files: %d", len(md.XcodeCacheDir.Files))
	logger.Infof("  Xcode cache symlinks: %d", len(md.XcodeCacheDir.Symlinks))
	logger.Infof("  Build Cache CLI version: %s", md.BuildCacheCLIVersion)
	logger.Infof("  Metadata version: %d", md.MetadataVersion)

	if isDebugLogMode {
		sortedDDFiles := make([]*xcode.FileInfo, len(md.DerivedData.Files))
		copy(sortedDDFiles, md.DerivedData.Files)

		sort.Slice(sortedDDFiles, func(i, j int) bool {
			return sortedDDFiles[i].Size > sortedDDFiles[j].Size
		})

		if len(sortedDDFiles) > 10 {
			sortedDDFiles = sortedDDFiles[:10]
		}

		logger.Debugf("  Largest files:")
		for i, file := range sortedDDFiles {
			logger.Debugf("    %d. %s (%s)", i+1, file.Path, humanize.Bytes(uint64(file.Size)))
		}
	}
}

func logCurrentUserInfo(logger log.Logger) {
	currentUser, err := user.Current()
	if err != nil {
		logger.Debugf("Error getting current user: %v", err)
	}

	logger.Debugf("Current user info:")
	logger.Debugf("  UID: %s", currentUser.Uid)
	logger.Debugf("  GID: %s", currentUser.Gid)
	logger.Debugf("  Username: %s", currentUser.Username)
}
