// Package daemon installs the Bitrise Build Cache helper processes (xcelerate
// proxy + ccache storage helper) as long-lived OS-supervised services. On
// macOS this means per-user LaunchAgent plists under
// ~/Library/LaunchAgents bootstrapped via launchctl. Linux support lives in
// ACI-5031 and is intentionally not implemented here.
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

// LogDirRelative is the path beneath the user's home where daemon stdout/stderr
// goes. Chosen distinct from ~/.local/state/xcelerate/logs (which the proxy
// writes itself) so we can clean up daemon-supervisor noise without nuking
// per-invocation proxy logs.
const LogDirRelative = ".local/state/bitrise-build-cache/logs"

// LaunchAgentsDirRelative is the per-user LaunchAgents location on macOS.
const LaunchAgentsDirRelative = "Library/LaunchAgents"

// Paths resolves the install locations for a given home directory. Kept as a
// struct so tests can construct it with t.TempDir() without touching the real
// $HOME.
type Paths struct {
	Home string
}

// NewPathsFromHome returns Paths rooted at the supplied home dir.
func NewPathsFromHome(home string) Paths {
	return Paths{Home: home}
}

// NewPaths returns Paths rooted at the current user's home dir.
func NewPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user home dir: %w", err)
	}

	return Paths{Home: home}, nil
}

// LaunchAgentsDir is the absolute path of ~/Library/LaunchAgents.
func (p Paths) LaunchAgentsDir() string {
	return filepath.Join(p.Home, LaunchAgentsDirRelative)
}

// LogDir is the absolute path of ~/.local/state/bitrise-build-cache/logs.
func (p Paths) LogDir() string {
	return filepath.Join(p.Home, LogDirRelative)
}

// PlistPath returns the .plist file path for a service label.
func (p Paths) PlistPath(label string) string {
	return filepath.Join(p.LaunchAgentsDir(), label+".plist")
}

// StdoutPath returns the stdout log file path for a service.
func (p Paths) StdoutPath(service string) string {
	return filepath.Join(p.LogDir(), service+".out.log")
}

// StderrPath returns the stderr log file path for a service.
func (p Paths) StderrPath(service string) string {
	return filepath.Join(p.LogDir(), service+".err.log")
}
