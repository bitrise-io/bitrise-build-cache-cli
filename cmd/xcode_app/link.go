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
	Long: `link appends ` + "`#include? \"<override>\"`" + ` to each in-tree .xcconfig under the project (or, for a .xcworkspace, each referenced project) so Xcode picks up the override on the next build. ` +
		`Engages the local compile-cache plugin — real speedup on incremental IDE builds. ` +
		`Does NOT engage remote CAS on Xcode 26+ IDE builds; Xcode drops COMPILATION_CACHE_REMOTE_SERVICE_PATH before writing .cas-config. See docs/xcode-app-ide-remote-cas-findings-2026-07-21.md. Remote engages via ` + "`xcodebuild`" + ` CLI (wrapper installed by ` + "`activate xcode`" + `). ` +
		`Optional-include form (` + "`#include?`" + `) keeps the change safe to commit — teammates or CI without the CLI silently skip it. ` +
		`When the project has no in-tree xcconfigs, falls back to writing a sibling bridge xcconfig (` + xa.BridgeXCConfigName + `) that the user must select as base configuration in Xcode. ` +
		`Idempotent — re-running is safe.`,
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

		printLinkResult(logger, args[0], result)

		return nil
	},
}

//nolint:gochecknoglobals
var unlinkCmd = &cobra.Command{
	Use:   "unlink <path>",
	Short: "Revert the Bitrise Build Cache include added by `xcode-app link`",
	Long: `unlink strips the Bitrise cache trailer from each xcconfig that ` + "`link`" + ` touched, and removes any sibling bridge xcconfig from the fallback path. ` +
		`Idempotent — nothing to revert reports a no-op.`,
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

		printUnlinkResult(logger, result)

		return nil
	},
}

func printLinkResult(logger log.Logger, requestedPath string, result xapkg.LinkResult) {
	if len(result.ModifiedXCConfigs) > 0 {
		logger.Donef("Auto-appended cache override include to %d project xcconfig file(s):", len(result.ModifiedXCConfigs))
		for _, f := range result.ModifiedXCConfigs {
			logger.Infof("  - %s", f)
		}
	}

	for _, f := range result.AlreadyLinked {
		logger.Infof("Already up to date: %s", f)
	}

	if len(result.BridgeFiles) > 0 {
		for _, f := range result.BridgeFiles {
			logger.Donef("Wrote sibling bridge xcconfig: %s", f)
		}
		logger.Infof("No in-tree xcconfig files were found — falling back to sibling-bridge mode.")
		logger.Infof("Next step in Xcode > project > Info tab > Configurations: set 'Based on Configuration File' to '%s' for each configuration.", strings.TrimSuffix(xa.BridgeXCConfigName, ".xcconfig"))
	}

	logger.Infof("Include target: %s", result.OverrideXCConfigPath)
	logger.Infof("Optional include (`#include?`) — teammates without the CLI silently skip the override.")

	if len(result.ModifiedXCConfigs) > 0 || len(result.AlreadyLinked) > 0 {
		logger.Infof("Rebuild via Cmd+B — local compile cache engages. Remote CAS on Xcode 26+ IDE is NOT engaged (Xcode's build system strips COMPILATION_CACHE_REMOTE_SERVICE_PATH); use `xcodebuild` CLI for remote.")
	}

	logger.Infof("Revert with: `bitrise-build-cache xcode-app unlink %s`.", requestedPath)
}

func printUnlinkResult(logger log.Logger, result xapkg.UnlinkResult) {
	for _, f := range result.ModifiedXCConfigs {
		logger.Donef("Stripped cache override include from: %s", f)
	}

	for _, f := range result.RemovedBridgeFiles {
		logger.Donef("Removed sibling bridge xcconfig: %s", f)
	}

	for _, f := range result.MissingBridgeFiles {
		logger.Infof("No bridge to remove: %s", f)
	}

	if result.NoOp {
		logger.Infof("Nothing to revert.")
	}
}

func init() {
	xcodeAppCmd.AddCommand(linkCmd)
	xcodeAppCmd.AddCommand(unlinkCmd)
}
