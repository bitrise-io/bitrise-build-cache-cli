package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/filegroup"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"io"
	"os"
	"time"
)

var testEmptyCmd = &cobra.Command{
	Use:          "test-empty",
	Short:        "",
	Long:         ``,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)

		logger.Infof("(i) Check Auth Config")
		authConfig, err := common.ReadAuthConfigFromEnvironments(os.Getenv)
		if err != nil {
			return fmt.Errorf("read auth config from environments: %w", err)
		}

		err = testEmptyCmdFn(cmd.Context(), authConfig, logger)
		if err != nil {
			return fmt.Errorf("save Gradle config cache into Bitrise Build Cache: %w", err)
		}

		logger.TInfof("✅ Configufation cache saved into Bitrise Build Cache ")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(testEmptyCmd)
}

func testEmptyCmdFn(ctx context.Context, authConfig common.CacheAuthConfig, logger log.Logger) error {

	path := "testEmpty"

	_, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create empty file: %w", err)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("hash copy file content: %w", err)
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	logger.Infof("File hash: %s", hash)

	kvClient, err := createKVClient(ctx, uuid.NewString(), authConfig, os.Getenv, logger)
	if err != nil {
		return fmt.Errorf("create kv client: %w", err)
	}

	fi := filegroup.FileInfo{
		Path:       "testEmpty",
		Size:       0,
		Hash:       hash,
		ModTime:    time.Now(),
		Mode:       420,
		Attributes: nil,
	}
	fg := filegroup.Info{
		Files: []*filegroup.FileInfo{
			&fi,
		},
		Directories: nil,
		Symlinks:    nil,
	}

	_, err = kvClient.UploadFileGroupToBuildCache(ctx, fg)
	if err != nil {
		return fmt.Errorf("upload file group to build cache: %w", err)
	}

	err = os.Remove("testEmpty")
	if err != nil {
		return fmt.Errorf("remove empty file: %w", err)
	}

	_, err = kvClient.DownloadFileGroupFromBuildCache(ctx, fg, true, false, false, 100)
	if err != nil {
		return fmt.Errorf("download file group from build cache: %w", err)
	}

	stat, err := os.Stat("testEmpty")
	if err != nil {
		return fmt.Errorf("stat empty file: %w", err)
	}

	logger.Infof("File name: %s", stat.Name())
	logger.Infof("File size: %d", stat.Size())

	return nil
}
