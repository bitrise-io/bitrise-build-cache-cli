package file

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	pkgfile "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/file"
)

// nolint:gochecknoglobals,dupl
var saveFileCmd = &cobra.Command{
	Use:          "save-file",
	Short:        "Save a single file to the Bitrise Build Cache under the given key",
	Long:         `Save a single file to the Bitrise Build Cache under the given key. The file's content is uploaded under the cache key passed via --key.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		common.LogCurrentUserInfo(logger)

		logger.TInfof("Save file to Bitrise Build Cache")
		logger.Infof("(i) Debug mode and verbose logs: %t", common.IsDebugLogMode)

		cacheKey, _ := cmd.Flags().GetString("key")
		filePath, _ := cmd.Flags().GetString("file")

		helper := pkgfile.NewHelper(pkgfile.HelperParams{
			Logger:       logger,
			DebugLogging: common.IsDebugLogMode,
		})
		if err := helper.Save(cmd.Context(), cacheKey, filePath); err != nil {
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
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		common.LogCurrentUserInfo(logger)

		logger.TInfof("Restore file from Bitrise Build Cache")
		logger.Infof("(i) Debug mode and verbose logs: %t", common.IsDebugLogMode)

		cacheKey, _ := cmd.Flags().GetString("key")
		filePath, _ := cmd.Flags().GetString("file")

		helper := pkgfile.NewHelper(pkgfile.HelperParams{
			Logger:       logger,
			DebugLogging: common.IsDebugLogMode,
		})
		if err := helper.Restore(cmd.Context(), cacheKey, filePath); err != nil {
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
