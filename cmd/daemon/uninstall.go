package daemon

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/permhint"
)

//nolint:gochecknoglobals
var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Unregister the Bitrise Build Cache services from the OS supervisor",
	Long: `uninstall stops and removes the LaunchAgents (macOS) or systemd --user units (Linux) installed by ` +
		"`daemon install`" + `. Idempotent — missing services / files are not errors.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		backend, paths, err := resolveBackendAndPaths()
		if err != nil {
			return err
		}

		result, err := daemonpkg.Uninstall(cmd.Context(), backend, paths, daemonpkg.DefaultServices())
		if err != nil {
			if errors.Is(err, daemonpkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			permhint.PrintIfApplicable(logger, err)

			return fmt.Errorf("uninstall daemon: %w", err)
		}

		for _, st := range result.Statuses {
			if st.Removed {
				logger.Donef("%s — stopped and removed %s", st.Service.Name, st.ConfigPath)
			} else {
				logger.Infof("  %s — nothing to remove (config not present)", st.Service.Name)
			}
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(uninstallCmd)
}
