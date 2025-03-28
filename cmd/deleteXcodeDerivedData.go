package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/filegroup"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/hash"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// nolint: gochecknoglobals
var deleteXcodeDerivedDataCmd = &cobra.Command{
	Use:          "delete-xcode-deriveddata",
	Short:        "Deletes the DerivedData cache archive from the Bitrise Build Cache for a given key",
	Long:         `Deletes the DerivedData cache archive from the Bitrise Build Cache for a given key.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Delete the Xcode DerivedData archive from the Bitrise Build Cache")

		logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

		logger.Infof("(i) Checking parameters")
		cacheKey, _ := cmd.Flags().GetString("key")
		empty, _ := cmd.Flags().GetBool("empty")

		if err := deleteXcodeDerivedDataCmdFn(cmd.Context(), cacheKey, empty, logger,
			os.Getenv,
			func(name string, v ...string) (string, error) {
				output, err := exec.Command(name, v...).Output()

				return string(output), err
			}); err != nil {
			return fmt.Errorf("delete Xcode DerivedData into Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ DerivedData cache archive deleted from Bitrise Build Cache ")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(deleteXcodeDerivedDataCmd)

	deleteXcodeDerivedDataCmd.Flags().String("key", "", "The cache key to be delete (set to the Bitrise app's slug and current git branch by default)")
	deleteXcodeDerivedDataCmd.Flags().Bool("empty", false, "If true, upload an empty metadata")
}

func deleteXcodeDerivedDataCmdFn(ctx context.Context,
	providedCacheKey string,
	uploadEmpty bool,
	logger log.Logger,
	envProvider func(string) string,
	commandFunc func(string, ...string) (string, error)) error {
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

	kvClient, err := createKVClient(ctx,
		CreateKVClientParams{
			CacheOperationID: uuid.NewString(),
			ClientName:       ClientNameXcode,
			AuthConfig:       authConfig,
			EnvProvider:      envProvider,
			CommandFunc:      commandFunc,
			Logger:           logger,
		})
	if err != nil {
		return fmt.Errorf("create kv client: %w", err)
	}

	if !uploadEmpty {
		return deleteCacheKey(ctx, providedCacheKey, cacheKey, envProvider, kvClient, logger)
	}

	return uploadEmptyMetadata(ctx, providedCacheKey, cacheKey, envProvider, kvClient, logger)
}

func uploadEmptyMetadata(ctx context.Context, providedCacheKey, cacheKey string, envProvider common.EnvProviderFunc, client *kv.Client, logger log.Logger) error {
	logger.TInfof("Saving empty metadata file %s", XCodeCacheMetadataPath)
	_, err := xcode.SaveMetadata(&xcode.Metadata{
		ProjectFiles:         filegroup.Info{},
		DerivedData:          filegroup.Info{},
		XcodeCacheDir:        filegroup.Info{},
		CacheKey:             cacheKey,
		CreatedAt:            time.Now(),
		AppID:                envProvider("BITRISE_APP_SLUG"),
		BuildID:              envProvider("BITRISE_BUILD_SLUG"),
		GitCommit:            envProvider("BITRISE_GIT_COMMIT"),
		GitBranch:            envProvider("BITRISE_GIT_BRANCH"),
		BuildCacheCLIVersion: envProvider("BITRISE_BUILD_CACHE_CLI_VERSION"),
		MetadataVersion:      1,
	}, XCodeCacheMetadataPath, logger)
	if err != nil {
		return fmt.Errorf("save metadata: %w", err)
	}

	mdChecksum, err := hash.ChecksumOfFile(XCodeCacheMetadataPath)
	mdChecksumReader := strings.NewReader(mdChecksum)
	if err != nil {
		return fmt.Errorf("checksum of metadata file: %w", err)
	}

	logger.TInfof("Uploading metadata checksum of %s (%s) for key %s", XCodeCacheMetadataPath, mdChecksum, cacheKey)
	if err := client.UploadStreamToBuildCache(ctx, mdChecksumReader, cacheKey, mdChecksumReader.Size()); err != nil {
		return fmt.Errorf("upload metadata checksum to build cache: %w", err)
	}

	logger.TInfof("Uploading metadata content of %s for key %s", XCodeCacheMetadataPath, mdChecksum)
	if err := client.UploadFileToBuildCache(ctx, XCodeCacheMetadataPath, mdChecksum); err != nil {
		return fmt.Errorf("upload metadata content to build cache: %w", err)
	}

	if providedCacheKey == "" {
		fallbackCacheKey, err := xcode.GetCacheKey(envProvider, xcode.CacheKeyParams{IsFallback: true})
		if err != nil {
			logger.Warnf("Failed to get fallback cache key: %s", err)
		} else if fallbackCacheKey != "" && cacheKey != fallbackCacheKey {
			cacheKey = fallbackCacheKey
			mdChecksumReader = strings.NewReader(mdChecksum) // reset reader
			logger.TInfof("Uploading metadata checksum of %s (%s) for fallback key %s", XCodeCacheMetadataPath, mdChecksum, cacheKey)
			if err := client.UploadStreamToBuildCache(ctx, mdChecksumReader, cacheKey, mdChecksumReader.Size()); err != nil {
				return fmt.Errorf("upload metadata checksum to build cache: %w", err)
			}
		}
	}

	return nil
}

func deleteCacheKey(ctx context.Context, providedCacheKey, cacheKey string, envProvider common.EnvProviderFunc, client *kv.Client, logger log.Logger) error {
	logger.TInfof("Deleting cache key %s", cacheKey)
	err := client.Delete(ctx, cacheKey)
	if err != nil {
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.NotFound {
			return fmt.Errorf("delete cache key: %w", err)
		}
	}

	if providedCacheKey != "" {
		return nil
	}

	fallbackCacheKey, err := xcode.GetCacheKey(envProvider, xcode.CacheKeyParams{IsFallback: true})
	if err != nil {
		logger.Warnf("Failed to get fallback cache key: %s", err)

		return nil
	}

	if fallbackCacheKey == "" || cacheKey == fallbackCacheKey {
		return nil
	}

	cacheKey = fallbackCacheKey
	logger.TInfof("Deleting fallback cache key %s", cacheKey)
	if err := client.Delete(ctx, cacheKey); err != nil {
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.NotFound {
			return fmt.Errorf("delete cache key: %w", err)
		}
	}

	return nil
}
