package daemon

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/permhint"
)

// transientBinPrefixes mark filesystem locations whose contents the OS may prune between logins;
// embedding such a path in a LaunchAgent/systemd unit would leave the supervisor pointing at a missing binary.
//
//nolint:gochecknoglobals
var transientBinPrefixes = []string{
	"/tmp/",
	"/var/folders/",
	"/private/var/folders/",
	"/private/tmp/",
}

func isTransientBinaryPath(exe string) bool {
	for _, prefix := range transientBinPrefixes {
		if strings.HasPrefix(exe, prefix) {
			return true
		}
	}

	return false
}

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

		// Do NOT EvalSymlinks — embedding the symlinked path lets CLI upgrades land without rerunning install.
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve CLI executable path: %w", err)
		}

		if isTransientBinaryPath(exe) {
			stable, copyErr := daemonpkg.CopyCLIToStableBin(exe)
			if copyErr != nil {
				return fmt.Errorf("copy CLI to stable dir before daemon install: %w", copyErr)
			}

			logger.Donef("Copied CLI binary to %s (was on a transient path: %s)", stable, exe)
			exe = stable
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
