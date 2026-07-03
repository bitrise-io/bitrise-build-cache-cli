package daemon

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/permhint"
)

//nolint:gochecknoglobals
var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the Bitrise Build Cache background services",
	Long: `up starts the daemon services that were registered by ` + "`daemon install`" + `. ` +
		`Safe to rerun, but not a true no-op against an already-running daemon: on macOS the underlying ` + "`launchctl bootstrap`" + ` is preceded by a ` + "`launchctl bootout`" + `, which briefly stops + restarts each service (so a CLI binary upgrade is picked up). On Linux ` + "`systemctl --user enable --now`" + ` is a real no-op on an already-running unit. ` +
		`Errors with a "run install first" hint if the supervisor config files are missing from disk.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		backend, paths, err := resolveBackendAndPaths()
		if err != nil {
			return err
		}

		result, err := daemonpkg.Up(cmd.Context(), backend, paths, daemonpkg.DefaultServices())
		if err != nil {
			if errors.Is(err, daemonpkg.ErrNotInstalled) {
				return err //nolint:wrapcheck // sentinel
			}

			permhint.PrintIfApplicable(logger, err)

			return fmt.Errorf("daemon up: %w", err)
		}

		for _, st := range result.Statuses {
			logger.Donef("%s — started (%s)", st.Service.Name, result.BackendName)
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(upCmd)
}
