package xcode_app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
	xapkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode_app"
)

//nolint:gochecknoglobals
var linkCmd = &cobra.Command{
	Use:   "link <path>",
	Short: "Wire an Xcode project or workspace up to the Bitrise Build Cache override",
	Long: `link writes a per-project bridge xcconfig next to the given .xcodeproj (or, for a .xcworkspace, next to every referenced .xcodeproj). ` +
		`The bridge ` + "`#include`" + `s ~/.bitrise-xcelerate/xcode-app.xcconfig, so pointing Xcode's base configuration at the bridge picks up every cache flag. ` +
		`Needed on macOS 26+: ` + "`launchctl setenv XCODE_XCCONFIG_FILE`" + ` no longer propagates to GUI-launched Xcode.app, so the ` + "`xcode-app enable`" + ` env override alone is not enough for GUI builds. ` +
		`Idempotent — running twice is a no-op.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		activator := &xapkg.Activator{Logger: logger}

		result, err := activator.Link(cmd.Context(), args[0])
		if err != nil {
			if errors.Is(err, xapkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("xcode-app link: %w", err)
		}

		for _, b := range result.BridgeFiles {
			logger.Donef("Wrote bridge xcconfig: %s", b)
		}
		for _, b := range result.AlreadyLinked {
			logger.Infof("Bridge already up to date: %s", b)
		}

		logger.Infof("Bridge includes: %s", result.OverrideXCConfigPath)
		logger.Infof("")
		logger.Infof("Next step: set the bridge as the base configuration in Xcode.")
		logger.Infof("  1. Open the project (or workspace) in Xcode.")
		logger.Infof("  2. Select the project in the sidebar. In the Info tab, expand Configurations.")
		logger.Infof("  3. For EACH configuration (Debug, Release, ...): set")
		logger.Infof("     'Based on Configuration File' to '%s'.", strings.TrimSuffix(xa.BridgeXCConfigName, ".xcconfig"))
		logger.Infof("  4. Close Xcode. Reopen. Build (Cmd+B).")
		logger.Infof("")
		logger.Infof("To remove the bridge: `bitrise-build-cache xcode-app unlink %s`.", args[0])

		return nil
	},
}

//nolint:gochecknoglobals
var unlinkCmd = &cobra.Command{
	Use:   "unlink <path>",
	Short: "Remove the per-project Bitrise Build Cache bridge xcconfig",
	Long: `unlink deletes the bridge xcconfig written by ` + "`xcode-app link`" + ` next to the given .xcodeproj (or, for a .xcworkspace, next to every referenced .xcodeproj). ` +
		`Idempotent — a missing bridge is reported, not an error. ` +
		`You still need to unset the base configuration in Xcode (Info tab, Configurations) manually.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		activator := &xapkg.Activator{Logger: logger}

		result, err := activator.Unlink(cmd.Context(), args[0])
		if err != nil {
			if errors.Is(err, xapkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("xcode-app unlink: %w", err)
		}

		for _, b := range result.RemovedBridgeFiles {
			logger.Donef("Removed bridge xcconfig: %s", b)
		}
		for _, b := range result.MissingBridgeFiles {
			logger.Infof("No bridge to remove: %s", b)
		}

		if len(result.RemovedBridgeFiles) > 0 {
			logger.Infof("Next step: in Xcode > project > Info tab > Configurations, clear 'Based on Configuration File' for each configuration that was pointing at the bridge.")
		}

		return nil
	},
}

func init() {
	xcodeAppCmd.AddCommand(linkCmd)
	xcodeAppCmd.AddCommand(unlinkCmd)
}
