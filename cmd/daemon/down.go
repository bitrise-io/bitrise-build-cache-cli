package daemon

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
)

//nolint:gochecknoglobals
var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop the Bitrise Build Cache background services",
	Long: `down stops the daemon services without removing their supervisor config. ` +
		`Use ` + "`daemon up`" + ` to bring them back, or ` + "`daemon uninstall`" + ` to remove the config too. ` +
		`Idempotent against not-loaded / never-started services.

Cross-platform note: on macOS down boots the service out (it won't auto-restart on next login until ` + "`up`" + ` runs). ` +
		`On Linux down stops the unit but leaves it enabled, so it will come back on next login.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()

		backend, paths, err := resolveBackendAndPaths()
		if err != nil {
			return err
		}

		result, err := daemonpkg.Down(cmd.Context(), backend, paths, daemonpkg.DefaultServices())
		if err != nil {
			if errors.Is(err, daemonpkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			printPermissionHintIfApplicable(cmd.ErrOrStderr(), err)

			return fmt.Errorf("daemon down: %w", err)
		}

		for _, st := range result.Statuses {
			fmt.Fprintf(out, "✓ %s — stopped (%s)\n", st.Service.Name, result.BackendName)
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(downCmd)
}
