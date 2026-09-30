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
var enableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable the Bitrise Build Cache override for Xcode.app IDE builds",
	Long: `enable writes ~/.bitrise-xcelerate/xcode-app.xcconfig with the settings that ` +
		`route Xcode's CAS plugin at Bitrise's xcelerate-proxy, runs ` +
		"`launchctl setenv XCODE_XCCONFIG_FILE`" + ` so the next Xcode.app launch ` +
		`picks it up, and registers a LaunchAgent that reapplies the env var on every login. ` +
		`If a previous ` + "`XCODE_XCCONFIG_FILE`" + ` was already set, it is chained in via ` +
		"`#include?`" + `.

Run ` + "`bitrise-build-cache activate xcode`" + ` first so an xcelerate config with a proxy ` +
		`socket path exists on disk. If Xcode.app is running, relaunch it to pick up the new env.

The IDE remote-CAS coverage is partial: Xcode-native targets pick up the override, ` +
		`but SPM package targets do not inherit ` + "`XCODE_XCCONFIG_FILE`" + ` — those stay on the ` +
		`built-in local cache. See docs/xcode-app.md for details.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		activator := &xapkg.Activator{
			Logger: logger,
			Envs:   utils.AllEnvs(),
		}

		result, err := activator.Enable(cmd.Context())
		if err != nil {
			if errors.Is(err, xapkg.ErrUnsupportedPlatform) || errors.Is(err, xapkg.ErrXcelerateNotConfigured) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("xcode-app enable: %w", err)
		}

		logger.Donef("Wrote override xcconfig: %s", result.XCConfigPath)
		if result.PreviousXCConfigPath != "" {
			logger.Infof("Chained previous XCODE_XCCONFIG_FILE: %s", result.PreviousXCConfigPath)
		}
		logger.Donef("Set XCODE_XCCONFIG_FILE via launchctl (LaunchAgent: %s)", result.LaunchAgentPlistPath)
		logger.Donef("Proxy socket: %s", result.XcelerateProxySocket)

		if len(result.RunningXcodePIDs) > 0 {
			logger.Warnf("Xcode is currently running (pid %v). Quit and relaunch Xcode to pick up the cache override.", result.RunningXcodePIDs)
		} else {
			logger.Infof("Next launch of Xcode will pick up the override.")
		}

		return nil
	},
}

func init() {
	xcodeAppCmd.AddCommand(enableCmd)
}
