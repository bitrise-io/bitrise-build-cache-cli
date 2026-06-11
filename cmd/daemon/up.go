package daemon

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
)

//nolint:gochecknoglobals
var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the Bitrise Build Cache background services",
	Long: `up starts the daemon services that were registered by ` + "`daemon install`" + `. ` +
		`Idempotent — running ` + "`up`" + ` against an already-running daemon is a no-op. ` +
		`Errors with a "run install first" hint if the supervisor config files are missing from disk.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()

		backend, paths, err := resolveBackendAndPaths()
		if err != nil {
			return err
		}

		result, err := daemonpkg.Up(cmd.Context(), backend, paths, daemonpkg.DefaultServices())
		if err != nil {
			if errors.Is(err, daemonpkg.ErrUnsupportedPlatform) || errors.Is(err, daemonpkg.ErrNotInstalled) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("daemon up: %w", err)
		}

		for _, st := range result.Statuses {
			fmt.Fprintf(out, "✓ %s — started (%s)\n", st.Service.Name, result.BackendName)
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(upCmd)
}
