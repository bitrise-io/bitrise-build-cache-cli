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
	Short: "Enable the Bitrise Build Cache override for Xcode.app GUI builds",
	Long: `enable writes the override xcconfig (~/.bitrise-xcelerate/xcode-app.xcconfig), runs ` +
		"`launchctl setenv XCODE_XCCONFIG_FILE` " +
		`so future Xcode.app launches pick it up, registers a LaunchAgent that re-applies the override on every login, ` +
		`and starts the xcelerate-proxy daemon service. ` +
		`If you already have ` + "`XCODE_XCCONFIG_FILE`" + ` set, your previous file is chained in via ` + "`#include`" + `. ` +
		`Detects running Xcode and nudges you to relaunch so the new env takes effect. ` +
		`Run ` + "`bitrise-build-cache activate xcode`" + ` first to write the xcelerate config that supplies the proxy socket path.`,
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

		if result.ProxyStartError != nil {
			logger.Errorf("xcelerate-proxy daemon did NOT start: %s", result.ProxyStartError)
			logger.Errorf("Xcode will read the override, dial %s, and get no response — 100%% cache miss until the proxy is up.", result.XcelerateProxySocket)
			logger.Errorf("Recover: `bitrise-build-cache daemon install && bitrise-build-cache daemon up`, then re-run `xcode-app enable`.")
		} else {
			logger.Donef("Proxy socket: %s", result.XcelerateProxySocket)
		}

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
