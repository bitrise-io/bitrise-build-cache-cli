package updater

import (
	"os"
	"path/filepath"
)

// DaemonInstalled is a file-presence check — a stale plist/unit from a partial uninstall yields a benign false-positive.
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
