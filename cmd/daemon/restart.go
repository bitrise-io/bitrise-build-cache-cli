package daemon

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
)

//nolint:gochecknoglobals
var restartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Stop and start the Bitrise Build Cache background services",
	Long:         `restart is shorthand for ` + "`daemon down`" + ` followed by ` + "`daemon up`" + `. Errors with a "run install first" hint if the supervisor config files are missing from disk.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()

		backend, paths, err := resolveBackendAndPaths()
		if err != nil {
			return err
		}

		result, err := daemonpkg.Restart(cmd.Context(), backend, paths, daemonpkg.DefaultServices())
		if err != nil {
			if errors.Is(err, daemonpkg.ErrUnsupportedPlatform) || errors.Is(err, daemonpkg.ErrNotInstalled) {
				return err //nolint:wrapcheck // sentinel
			}

			printPermissionHintIfApplicable(cmd.ErrOrStderr(), err)

			return fmt.Errorf("daemon restart: %w", err)
		}

		for _, st := range result.Statuses {
			fmt.Fprintf(out, "✓ %s — restarted (%s)\n", st.Service.Name, result.BackendName)
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(restartCmd)
}
