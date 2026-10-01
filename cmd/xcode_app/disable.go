package xcode_app

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	xapkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode_app"
)

//nolint:gochecknoglobals
var disableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable the Bitrise Build Cache override for Xcode.app IDE builds",
	Long: `disable reverses "xcode-app enable": boots out the LaunchAgent, removes its plist,
unsets XCODE_XCCONFIG_FILE via launchctl, and removes the override xcconfig under
~/.bitrise-xcelerate/. Idempotent — safe to run when not enabled.

If Xcode.app is running, relaunch it to pick up the cleared env. Does NOT stop the
xcelerate-proxy — the "xcodebuild" wrapper flow depends on it.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		activator := &xapkg.Activator{
			Logger: logger,
			Envs:   utils.AllEnvs(),
		}

		result, err := activator.Disable(cmd.Context())
		if err != nil {
			if errors.Is(err, xapkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("xcode-app disable: %w", err)
		}

		switch {
		case result.XCConfigRemoved:
			logger.Donef("Removed override xcconfig + LaunchAgent")
		case result.LaunchAgentRemoved:
			logger.Donef("Removed LaunchAgent (no override xcconfig on disk)")
		}
		logger.Infof("Cleared XCODE_XCCONFIG_FILE via launchctl")

		if len(result.RunningXcodePIDs) > 0 {
			logger.Warnf("Xcode is currently running (pid %v). Quit and relaunch Xcode to pick up the cleared env.", result.RunningXcodePIDs)
		}

		return nil
	},
}

func init() {
	xcodeAppCmd.AddCommand(disableCmd)
}
