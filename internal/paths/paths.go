// Package paths centralises the on-disk locations the CLI reads and writes.
// One package so the layout under ~/.local/state/bitrise-build-cache stays
// consistent across versioncheck, refresh, daemon supervisor logs, and the
// future Xcelerate / ccache state dirs.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// Relative dirs under $HOME.
const (
	// StateDirRelative is the shared per-user state root.
	StateDirRelative = ".local/state/bitrise-build-cache"

	// LaunchAgentsDirRelative is macOS's per-user LaunchAgents location.
	LaunchAgentsDirRelative = "Library/LaunchAgents"

	// SystemdUserDirRelative is Linux's per-user systemd unit dir.
	SystemdUserDirRelative = ".config/systemd/user"

	// daemonLogsSubdir is the daemon supervisor stdout/stderr log dir.
	daemonLogsSubdir = "logs"
)

// Paths resolves on-disk locations rooted at a single home directory.
type Paths struct {
	Home string
}

// FromHome returns Paths rooted at the supplied home dir.
func FromHome(home string) Paths {
	return Paths{Home: home}
}

// Default returns Paths rooted at the current user's home dir.
func Default() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user home dir: %w", err)
	}

	return Paths{Home: home}, nil
}

// StateDir is the absolute path of the per-user state root.
func (p Paths) StateDir() string {
	return filepath.Join(p.Home, StateDirRelative)
}

// StateFile returns the absolute path of a file under StateDir.
func (p Paths) StateFile(name string) string {
	return filepath.Join(p.StateDir(), name)
}

// LaunchAgentsDir is the absolute path of the per-user macOS LaunchAgents dir.
func (p Paths) LaunchAgentsDir() string {
	return filepath.Join(p.Home, LaunchAgentsDirRelative)
}

// SystemdUserDir is the absolute path of the per-user systemd unit dir.
func (p Paths) SystemdUserDir() string {
	return filepath.Join(p.Home, SystemdUserDirRelative)
}

// DaemonLogDir is the absolute path of the daemon supervisor stdout/stderr log dir.
func (p Paths) DaemonLogDir() string {
	return filepath.Join(p.StateDir(), daemonLogsSubdir)
}

// PlistPath returns the per-user LaunchAgent plist path for the given label.
func (p Paths) PlistPath(label string) string {
	return filepath.Join(p.LaunchAgentsDir(), label+".plist")
}

// UnitPath returns the systemd --user unit file path for the given name.
func (p Paths) UnitPath(unitName string) string {
	return filepath.Join(p.SystemdUserDir(), unitName+".service")
}

// DaemonStdoutPath returns the supervisor stdout log file path for a service.
func (p Paths) DaemonStdoutPath(service string) string {
	return filepath.Join(p.DaemonLogDir(), service+".out.log")
}

// DaemonStderrPath returns the supervisor stderr log file path for a service.
func (p Paths) DaemonStderrPath(service string) string {
	return filepath.Join(p.DaemonLogDir(), service+".err.log")
}
