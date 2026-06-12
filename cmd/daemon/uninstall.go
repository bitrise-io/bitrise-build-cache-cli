package daemon

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
)

//nolint:gochecknoglobals
var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Unregister the Bitrise Build Cache services from the OS supervisor",
	Long: `uninstall stops and removes the LaunchAgents (macOS) or systemd --user units (Linux) installed by ` +
		"`daemon install`" + `. Idempotent — missing services / files are not errors.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()

		backend, err := daemonpkg.DefaultBackend()
		if err != nil {
			return err //nolint:wrapcheck // sentinel
		}

		paths, err := daemonpkg.NewPaths()
		if err != nil {
			return err //nolint:wrapcheck // already context-rich
		}

		result, err := daemonpkg.Uninstall(cmd.Context(), backend, paths, daemonpkg.DefaultServices())
		if err != nil {
			if errors.Is(err, daemonpkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			printPermissionHintIfApplicable(cmd.ErrOrStderr(), err)

			return fmt.Errorf("uninstall daemon: %w", err)
		}

		for _, st := range result.Statuses {
			if st.Removed {
				fmt.Fprintf(out, "✓ %s — stopped and removed %s\n", st.Service.Name, st.ConfigPath)
			} else {
				fmt.Fprintf(out, "  %s — nothing to remove (config not present)\n", st.Service.Name)
			}
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(uninstallCmd)
}
