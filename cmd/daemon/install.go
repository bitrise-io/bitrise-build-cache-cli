package daemon

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
)

//nolint:gochecknoglobals
var installCmd = &cobra.Command{
	Use:          "install",
	Short:        "Register the Bitrise Build Cache services with the OS supervisor",
	Long:         `install writes LaunchAgent plists for the xcelerate proxy and the ccache storage helper, then bootstraps them with launchctl. Safe to rerun after a CLI upgrade — the plists are rewritten and the services restarted.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()

		paths, err := daemonpkg.NewPaths()
		if err != nil {
			return err //nolint:wrapcheck // already context-rich
		}

		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve CLI executable path: %w", err)
		}

		result, err := daemonpkg.Install(cmd.Context(), daemonpkg.ExecRunner{}, paths, daemonpkg.DefaultServices(), exe)
		if err != nil {
			if errors.Is(err, daemonpkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel; preserve identity
			}

			return fmt.Errorf("install daemon: %w", err)
		}

		for _, st := range result.Statuses {
			fmt.Fprintf(out, "✓ %s — wrote %s and bootstrapped\n", st.Service.Label(), st.PlistPath)
		}

		fmt.Fprintln(out)
		fmt.Fprintln(out, "Services are now running. Logs:")
		fmt.Fprintf(out, "  %s\n", paths.LogDir())
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Verify with: launchctl print gui/$UID/io.bitrise.build-cache.xcelerate-proxy")

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(installCmd)
}
