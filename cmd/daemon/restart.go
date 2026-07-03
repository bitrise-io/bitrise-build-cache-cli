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
var restartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Stop and start the Bitrise Build Cache background services",
	Long:         `restart is shorthand for ` + "`daemon down`" + ` followed by ` + "`daemon up`" + `. Errors with a "run install first" hint if the supervisor config files are missing from disk.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		backend, paths, err := resolveBackendAndPaths()
		if err != nil {
			return err
		}

		result, err := daemonpkg.Restart(cmd.Context(), backend, paths, daemonpkg.DefaultServices())
		if err != nil {
			if errors.Is(err, daemonpkg.ErrNotInstalled) {
				return err //nolint:wrapcheck // sentinel
			}

			permhint.PrintIfApplicable(logger, err)

			return fmt.Errorf("daemon restart: %w", err)
		}

		for _, st := range result.Statuses {
			logger.Donef("%s — restarted (%s)", st.Service.Name, result.BackendName)
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(restartCmd)
}
