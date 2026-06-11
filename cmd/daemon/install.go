package daemon

import (
	"errors"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
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
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		backend, paths, err := resolveBackendAndPaths()
		if err != nil {
			return err
		}

		// os.Executable returns the path used to start the process, NOT a
		// symlink-resolved canonical path. We embed exactly what's returned
		// into the supervisor config (LaunchAgent plist / systemd ExecStart)
		// so an `installer.sh -b ~/.local/bin` install that symlinks the
		// binary will be re-invoked through the same symlink on every
		// launchd / systemd start. That's the right behaviour: a CLI upgrade
		// that swaps the symlink target lands automatically without needing
		// `daemon install` to re-write the plist. If we EvalSymlinks'd here,
		// upgrades would only land after rerunning install.
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve CLI executable path: %w", err)
		}

		result, err := daemonpkg.Install(cmd.Context(), backend, paths, daemonpkg.DefaultServices(), exe)
		if err != nil {
			if errors.Is(err, daemonpkg.ErrUnsupportedPlatform) {
				return err //nolint:wrapcheck // sentinel
			}

			// Surface the actionable hint before returning the raw error so
			// the user sees the chown / remove-and-retry remediation
			// alongside the offending path.
			printPermissionHintIfApplicable(logger, err)

			return fmt.Errorf("install daemon: %w", err)
		}

		for _, st := range result.Statuses {
			logger.Donef("%s — wrote %s (%s)", st.Service.Name, st.ConfigPath, result.BackendName)
		}

		logger.Println()
		logger.Infof("Services are now running. Logs:")

		switch result.BackendName {
		case "launchd":
			logger.Infof("  %s", paths.LogDir())
			logger.Println()
			logger.Infof("Verify with: launchctl print gui/$UID/io.bitrise.build-cache.xcelerate-proxy")
		case "systemd":
			logger.Infof("  journalctl --user -u bitrise-build-cache-xcelerate-proxy")
			logger.Println()
			logger.Infof("Verify with: systemctl --user status bitrise-build-cache-xcelerate-proxy")
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(installCmd)
}
