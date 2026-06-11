package daemon

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
)

//nolint:gochecknoglobals
var uninstallCmd = &cobra.Command{
	Use:          "uninstall",
	Short:        "Unregister the Bitrise Build Cache services from the OS supervisor",
	Long:         `uninstall boots out the LaunchAgents installed by ` + "`daemon install`" + ` and removes their plist files. Idempotent — missing services / files are not errors.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()

		paths, err := daemonpkg.NewPaths()
		if err != nil {
			return err //nolint:wrapcheck // already context-rich
		}

		result, err := daemonpkg.Uninstall(cmd.Context(), daemonpkg.ExecRunner{}, paths, daemonpkg.DefaultServices())
		if err != nil {
			if errors.Is(err, daemonpkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("uninstall daemon: %w", err)
		}

		for _, st := range result.Statuses {
			if st.Removed {
				fmt.Fprintf(out, "✓ %s — booted out and removed %s\n", st.Service.Label(), st.PlistPath)
			} else {
				fmt.Fprintf(out, "  %s — nothing to remove (plist not present)\n", st.Service.Label())
			}
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(uninstallCmd)
}
