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
	Use:   "install",
	Short: "Register the Bitrise Build Cache services with the OS supervisor",
	Long: `install registers the xcelerate proxy and the ccache storage helper with the host OS's per-user supervisor: ` +
		`LaunchAgents on macOS, systemd --user units on Linux. ` +
		`Safe to rerun after a CLI upgrade — the supervisor configs are rewritten and the services restarted.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()

		backend, err := daemonpkg.DefaultBackend()
		if err != nil {
			return err //nolint:wrapcheck // sentinel; preserve identity
		}

		paths, err := daemonpkg.NewPaths()
		if err != nil {
			return err //nolint:wrapcheck // already context-rich
		}

		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve CLI executable path: %w", err)
		}

		result, err := daemonpkg.Install(cmd.Context(), backend, paths, daemonpkg.DefaultServices(), exe)
		if err != nil {
			if errors.Is(err, daemonpkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("install daemon: %w", err)
		}

		for _, st := range result.Statuses {
			fmt.Fprintf(out, "✓ %s — wrote %s (%s)\n", st.Service.Name, st.ConfigPath, result.BackendName)
		}

		fmt.Fprintln(out)
		fmt.Fprintln(out, "Services are now running. Logs:")

		switch result.BackendName {
		case "launchd":
			fmt.Fprintf(out, "  %s\n", paths.LogDir())
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Verify with: launchctl print gui/$UID/io.bitrise.build-cache.xcelerate-proxy")
		case "systemd":
			fmt.Fprintln(out, "  journalctl --user -u bitrise-build-cache-xcelerate-proxy")
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Verify with: systemctl --user status bitrise-build-cache-xcelerate-proxy")
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(installCmd)
}
