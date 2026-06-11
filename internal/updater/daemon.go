package updater

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DaemonInstalled reports whether the user has any daemon supervisor config
// on disk. Used after an upgrade to surface the restart hint only when it's
// actually relevant.
//
// macOS: checks for either io.bitrise.build-cache.xcelerate-proxy.plist or
// io.bitrise.build-cache.ccache-helper.plist under ~/Library/LaunchAgents.
//
// Linux: checks for either bitrise-build-cache-xcelerate-proxy.service or
// bitrise-build-cache-ccache-helper.service under ~/.config/systemd/user.
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

// PrintDaemonRestartHint emits a one-line "you should restart the daemon"
// nudge. Caller is responsible for gating on DaemonInstalled(home).
func PrintDaemonRestartHint(w io.Writer) {
	_, _ = fmt.Fprintln(w,
		"You have the Bitrise Build Cache daemon installed. Run `bitrise-build-cache daemon restart` to pick up the new binary.",
	)
}
