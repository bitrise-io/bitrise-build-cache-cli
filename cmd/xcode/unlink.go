package xcode

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	xapkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode_app"
)

//nolint:gochecknoglobals
var unlinkCmd = &cobra.Command{
	Use:   "unlink <path>",
	Short: "Revert the Bitrise Build Cache include added by `xcode link`",
	Long: `unlink removes the marker-fenced include block from every xcconfig that "link"
touched, and removes any sibling .bitrise-build-cache.xcconfig that is now
empty. The project's baseConfigurationReference is left in place — pbxproj
gives us no way to distinguish "the user already had it" from "we set it".

Idempotent — nothing to revert reports a no-op.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		activator := &xapkg.Activator{
			Logger: logger,
			Envs:   utils.AllEnvs(),
		}

		result, err := activator.Unlink(cmd.Context(), args[0])
		if err != nil {
			if errors.Is(err, xapkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("xcode unlink: %w", err)
		}

		for _, f := range result.ModifiedXCConfigs {
			logger.Donef("Stripped include block from: %s", f)
		}
		for _, f := range result.RemovedSiblings {
			logger.Donef("Removed sibling xcconfig: %s", f)
		}
		if len(result.WarnBaseRefs) > 0 {
			logger.Warnf("baseConfigurationReference entries left in place (pbxproj offers no way to tell apart user-set vs. link-set): %s",
				strings.Join(result.WarnBaseRefs, ", "))
		}
		if len(result.ModifiedXCConfigs) == 0 && len(result.RemovedSiblings) == 0 {
			logger.Infof("Nothing to revert.")
		}

		return nil
	},
}

func init() {
	xcodeCommand.AddCommand(unlinkCmd)
}
