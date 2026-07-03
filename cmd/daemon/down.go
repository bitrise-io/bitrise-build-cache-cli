package daemon

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/permhint"
)

//nolint:gochecknoglobals
var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop the Bitrise Build Cache background services",
	Long: `down stops the daemon services without removing their supervisor config. ` +
		`Use ` + "`daemon up`" + ` to bring them back, or ` + "`daemon uninstall`" + ` to remove the config too. ` +
		`Idempotent against not-loaded / never-started services. Both platforms re-bootstrap services on the next login.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		backend, paths, err := resolveBackendAndPaths()
		if err != nil {
			return err
		}

		result, err := daemonpkg.Down(cmd.Context(), backend, paths, daemonpkg.DefaultServices())
		if err != nil {
			permhint.PrintIfApplicable(logger, err)

			return fmt.Errorf("daemon down: %w", err)
		}

		for _, st := range result.Statuses {
			logger.Donef("%s — stopped (%s)", st.Service.Name, result.BackendName)
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(downCmd)
}
