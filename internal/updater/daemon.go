package updater

import (
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"
)

// DaemonInstalled reports whether the user has any daemon supervisor config on disk.
//
// File-presence check only — a stale plist/unit file from a partial uninstall yields
// a benign false-positive (the resulting `daemon restart` is a no-op against a not-loaded service).
func DaemonInstalled(home string) bool {
	candidates := []string{
		filepath.Join(home, "Library", "LaunchAgents", "io.bitrise.build-cache.xcelerate-proxy.plist"),
		filepath.Join(home, "Library", "LaunchAgents", "io.bitrise.build-cache.ccache-helper.plist"),
		filepath.Join(home, ".config", "systemd", "user", "bitrise-build-cache-xcelerate-proxy.service"),
		filepath.Join(home, ".config", "systemd", "user", "bitrise-build-cache-ccache-helper.service"),
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}

	return false
}

// PrintDaemonRestartHint emits the restart nudge. Caller gates on DaemonInstalled(home).
func PrintDaemonRestartHint(logger log.Logger) {
	if logger == nil {
		return
	}

	logger.Infof("You have the Bitrise Build Cache daemon installed. Run `bitrise-build-cache daemon restart` to pick up the new binary.")
}
