package xcode_app

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	xapkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/xcode_app"
)

//nolint:gochecknoglobals
var disableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable the Bitrise Build Cache override for Xcode.app GUI builds",
	Long: `disable reverses ` + "`xcode-app enable`" + `: boots out the LaunchAgent, removes its plist, ` +
		`removes the override xcconfig under ~/.bitrise-xcelerate/, and restores the prior XCODE_XCCONFIG_FILE ` +
		`environment value (if one was captured at enable time). Idempotent — safe to run when not enabled.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		activator := &xapkg.Activator{Logger: logger}

		result, err := activator.Disable(cmd.Context())
		if err != nil {
			if errors.Is(err, xapkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("xcode-app disable: %w", err)
		}

		if result.LaunchAgentRemoved {
			logger.Donef("Removed LaunchAgent + xcconfig override")
		}

		if result.RestoredXCConfigPath != "" {
			logger.Infof("Restored prior XCODE_XCCONFIG_FILE: %s", result.RestoredXCConfigPath)
		} else {
			logger.Infof("Cleared XCODE_XCCONFIG_FILE (no prior override to restore)")
		}

		return nil
	},
}

func init() {
	xcodeAppCmd.AddCommand(disableCmd)
}
