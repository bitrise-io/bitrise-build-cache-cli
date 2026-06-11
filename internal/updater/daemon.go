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
//
// Limitation: this is a file-presence check, NOT a "service is actually
// loaded" check. A stale plist / unit file left behind by a partial
// `daemon uninstall` (or by a user who manually `launchctl bootout`ed and
// forgot to delete the file) triggers a false-positive restart hint. The
// hint itself is benign — running `daemon restart` against a not-loaded
// service is a no-op on launchd (bootout-then-bootstrap brings it back up
// cleanly) and effectively `start` on systemd. Worth promoting to a real
// `launchctl print` / `systemctl --user status` check if the false-positive
// rate gets noisy in practice.
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
