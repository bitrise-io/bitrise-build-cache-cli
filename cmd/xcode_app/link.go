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
var linkCmd = &cobra.Command{
	Use:   "link <path>",
	Short: "Wire an Xcode project or workspace to the Bitrise Build Cache override xcconfig",
	Long: `link patches a .xcodeproj (or every .xcodeproj in a .xcworkspace) so Xcode.app's
IDE builds pick up the override xcconfig written by "activate xcode". Required on
Xcode 27+ — Xcode no longer propagates the XCODE_XCCONFIG_FILE user-env override to
SwiftBuild, so the override needs to be reachable through the project's
baseConfigurationReference chain.

For each XCBuildConfiguration:
  - If it already has a baseConfigurationReference, append a marker-fenced
    "#include?" directive to that xcconfig.
  - Otherwise, create a sibling .bitrise-build-cache.xcconfig next to the .xcodeproj,
    set the baseConfigurationReference to it, and "#include?" the override from there.

Idempotent — re-running replaces the marker block cleanly. SPM package targets are
NOT reached by this command (architectural limitation).`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		activator := &xapkg.Activator{
			Logger: logger,
			Envs:   utils.AllEnvs(),
		}

		result, err := activator.Link(cmd.Context(), args[0])
		if err != nil {
			if errors.Is(err, xapkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("xcode-app link: %w", err)
		}

		for _, f := range result.ModifiedXCConfigs {
			logger.Donef("Added include block to: %s", f)
		}
		for _, f := range result.CreatedSiblings {
			logger.Donef("Created sibling xcconfig + patched project: %s", f)
		}
		if len(result.ModifiedXCConfigs) == 0 && len(result.CreatedSiblings) == 0 {
			logger.Infof("Nothing to change — project already linked.")
		}

		return nil
	},
}

func init() {
	xcodeAppCmd.AddCommand(linkCmd)
}
