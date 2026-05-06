package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/build_cache/kv"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

// nolint:gochecknoglobals,dupl
var saveFileCmd = &cobra.Command{
	Use:          "save-file",
	Short:        "Save a single file to the Bitrise Build Cache under the given key",
	Long:         `Save a single file to the Bitrise Build Cache under the given key. The file's content is uploaded under the cache key passed via --key.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(common.IsDebugLogMode)
		common.LogCurrentUserInfo(logger)

		logger.TInfof("Save file to Bitrise Build Cache")
		logger.Infof("(i) Debug mode and verbose logs: %t", common.IsDebugLogMode)

		cacheKey, _ := cmd.Flags().GetString("key")
		filePath, _ := cmd.Flags().GetString("file")

		if err := saveFile(cmd.Context(), cacheKey, filePath, logger); err != nil {
			return fmt.Errorf("save file to Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ File saved to Bitrise Build Cache")

		return nil
	},
}

// nolint:gochecknoglobals,dupl
var restoreFileCmd = &cobra.Command{
	Use:          "restore-file",
	Short:        "Restore a single file from the Bitrise Build Cache by key",
	Long:         `Restore a single file from the Bitrise Build Cache by key. The content stored under --key is downloaded and written to --file.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(common.IsDebugLogMode)
		common.LogCurrentUserInfo(logger)

		logger.TInfof("Restore file from Bitrise Build Cache")
		logger.Infof("(i) Debug mode and verbose logs: %t", common.IsDebugLogMode)

		cacheKey, _ := cmd.Flags().GetString("key")
		filePath, _ := cmd.Flags().GetString("file")

		if err := restoreFile(cmd.Context(), cacheKey, filePath, logger); err != nil {
			return fmt.Errorf("restore file from Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ File restored from Bitrise Build Cache")

		return nil
	},
}

func init() {
	common.RootCmd.AddCommand(saveFileCmd)
	saveFileCmd.Flags().String("key", "", "The cache key under which the file will be stored (required)")
	saveFileCmd.Flags().String("file", "", "Path to the file to upload (required)")
	_ = saveFileCmd.MarkFlagRequired("key")
	_ = saveFileCmd.MarkFlagRequired("file")

	common.RootCmd.AddCommand(restoreFileCmd)
	restoreFileCmd.Flags().String("key", "", "The cache key under which the file is stored (required)")
	restoreFileCmd.Flags().String("file", "", "Path where the restored file will be written (required)")
	_ = restoreFileCmd.MarkFlagRequired("key")
	_ = restoreFileCmd.MarkFlagRequired("file")
}

func newKVClient(ctx context.Context, logger log.Logger) (*kv.Client, error) {
	logger.Infof("(i) Check Auth Config")
	allEnvs := utils.AllEnvs()
	authConfig, err := configcommon.ReadAuthConfigFromEnvironments(allEnvs)
	if err != nil {
		return nil, fmt.Errorf("read auth config from environments: %w", err)
	}

	kvClient, err := common.CreateKVClient(ctx,
		common.CreateKVClientParams{
			CacheOperationID: uuid.NewString(),
			ClientName:       common.ClientNameFile,
			AuthConfig:       authConfig,
			Envs:             allEnvs,
			CommandFunc: func(name string, v ...string) (string, error) {
				output, err := exec.CommandContext(ctx, name, v...).Output()

				return string(output), err
			},
			Logger: logger,
		})
	if err != nil {
		return nil, fmt.Errorf("create kv client: %w", err)
	}

	return kvClient, nil
}

func saveFile(ctx context.Context, cacheKey, filePath string, logger log.Logger) error {
	if cacheKey == "" {
		return errors.New("--key is required")
	}
	if filePath == "" {
		return errors.New("--file is required")
	}

	if _, err := os.Stat(filePath); err != nil {
		return fmt.Errorf("stat file %q: %w", filePath, err)
	}

	logger.Infof("(i) Cache key: %s", cacheKey)
	logger.Infof("(i) File: %s", filePath)

	kvClient, err := newKVClient(ctx, logger)
	if err != nil {
		return err
	}

	logger.TInfof("Uploading %s for key %s", filePath, cacheKey)
	if err := kvClient.UploadFileToBuildCache(ctx, filePath, cacheKey); err != nil {
		return fmt.Errorf("upload file to build cache: %w", err)
	}

	return nil
}

func restoreFile(ctx context.Context, cacheKey, filePath string, logger log.Logger) error {
	if cacheKey == "" {
		return errors.New("--key is required")
	}
	if filePath == "" {
		return errors.New("--file is required")
	}

	logger.Infof("(i) Cache key: %s", cacheKey)
	logger.Infof("(i) File: %s", filePath)

	kvClient, err := newKVClient(ctx, logger)
	if err != nil {
		return err
	}

	logger.TInfof("Downloading %s for key %s", filePath, cacheKey)
	if err := kvClient.DownloadFileFromBuildCache(ctx, filePath, cacheKey); err != nil {
		if errors.Is(err, kv.ErrCacheNotFound) {
			return fmt.Errorf("no cache item found for key %q: %w", cacheKey, err)
		}

		return fmt.Errorf("download file from build cache: %w", err)
	}

	return nil
}
