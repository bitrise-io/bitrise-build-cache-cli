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
var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Register the Bitrise Build Cache services with the OS supervisor",
	Long: `install registers the xcelerate proxy and the ccache storage helper with the host OS's per-user supervisor: ` +
		`LaunchAgents on macOS, systemd --user units on Linux. ` +
		`Safe to rerun after a CLI upgrade — the supervisor configs are rewritten and the services restarted.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		backend, paths, err := resolveBackendAndPaths()
		if err != nil {
			return err
		}

		exe, err := daemonpkg.ResolveSupervisedBinary(logger)
		if err != nil {
			return err //nolint:wrapcheck // already context-rich
		}

		result, err := daemonpkg.Install(cmd.Context(), backend, paths, daemonpkg.DefaultServices(), exe)
		if err != nil {
			if errors.Is(err, daemonpkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			permhint.PrintIfApplicable(logger, err)

			return fmt.Errorf("install daemon: %w", err)
		}

		for _, st := range result.Statuses {
			logger.Donef("%s — wrote %s (%s)", st.Service.Name, st.ConfigPath, result.BackendName)
		}

		logger.Println()
		logger.Infof("Services are now running.")

		switch result.BackendName {
		case "launchd":
			logger.Infof("Supervisor stdout/stderr log dir: %s", paths.DaemonLogDir())
			logger.Println()
			logger.Infof("Verify with: launchctl print gui/$UID/io.bitrise.build-cache.xcelerate-proxy")
		case "systemd":
			logger.Infof("Supervisor log stream: journalctl --user -u bitrise-build-cache-xcelerate-proxy")
			logger.Println()
			logger.Infof("Verify with: systemctl --user status bitrise-build-cache-xcelerate-proxy")
		}

		logger.Println()
		logger.Infof("Socket paths (for IDE configuration): bitrise-build-cache daemon info")

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(installCmd)
}
