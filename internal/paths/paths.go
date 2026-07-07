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

	// BitriseRootRelative is the shared per-user dir under $HOME holding the cache root + stable CLI binary.
	BitriseRootRelative = ".bitrise"

	// XcelerateRootRelative is the per-user Xcelerate config root (~/.bitrise-xcelerate).
	XcelerateRootRelative = ".bitrise-xcelerate"

	// ProxySocketName is the xcelerate proxy unix-socket filename (lives under the OS temp dir).
	ProxySocketName = "xcelerate-proxy.sock"

	// ProxyPidFileName is the xcelerate proxy pid file written into XcelerateRoot.
	ProxyPidFileName = "proxy.pid"

	// CcacheSocketName is the ccache IPC unix-socket filename (lives under the OS temp dir).
	CcacheSocketName = "ccache-ipc.sock"

	// xcelerateLogsRelative is the per-user xcelerate log dir.
	xcelerateLogsRelative = ".local/state/xcelerate/logs"

	// ccacheLogsRelative is the per-user ccache log dir.
	ccacheLogsRelative = ".local/state/ccache/logs"

	// daemonLogsSubdir is the daemon supervisor stdout/stderr log dir.
	daemonLogsSubdir = "logs"

	// invocationsSubdir holds the per-day NDJSON invocation log files.
	invocationsSubdir = "invocations"

	// bitriseBinSubdir holds the stable CLI binary copy used by the daemon supervisor.
	bitriseBinSubdir = "bin"

	// bitriseCacheSubdir is the per-tool cache/marker root used by activate, refresh, and child-stats.
	bitriseCacheSubdir = "cache"

	// xcelerateBinSubdir holds the xcelerate wrapper scripts (xcodebuild / xcrun) and CLI copy.
	xcelerateBinSubdir = "bin"

	// xcelerateConfigFile is the JSON config file written by `activate xcode`.
	xcelerateConfigFile = "config.json"

	// gradleInitScriptRelative is the per-user gradle init script written by `activate gradle`.
	gradleInitScriptRelative = ".gradle/init.d/bitrise-build-cache.init.gradle.kts"
)

// CLIBinaryName is the on-disk name of the CLI executable (daemon plist entry, PATH lookup).
const CLIBinaryName = "bitrise-build-cache"

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

func (p Paths) InvocationsDir() string {
	return filepath.Join(p.StateDir(), invocationsSubdir)
}

func (p Paths) InvocationsFile(day string) string {
	return filepath.Join(p.InvocationsDir(), day+".ndjson")
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

// BitriseRoot is the absolute path of the per-user ~/.bitrise dir.
func (p Paths) BitriseRoot() string {
	return filepath.Join(p.Home, BitriseRootRelative)
}

// BitriseBinDir is the absolute path of ~/.bitrise/bin (stable CLI copy).
func (p Paths) BitriseBinDir() string {
	return filepath.Join(p.BitriseRoot(), bitriseBinSubdir)
}

// BitriseBinFile returns a file path under BitriseBinDir.
func (p Paths) BitriseBinFile(name string) string {
	return filepath.Join(p.BitriseBinDir(), name)
}

// BitriseCacheDir is the per-tool cache/marker dir under ~/.bitrise/cache.
func (p Paths) BitriseCacheDir(tool string) string {
	return filepath.Join(p.BitriseRoot(), bitriseCacheSubdir, tool)
}

// BitriseCacheFile returns a file path under BitriseCacheDir(tool).
func (p Paths) BitriseCacheFile(tool, name string) string {
	return filepath.Join(p.BitriseCacheDir(tool), name)
}

// XcelerateRoot is the absolute path of ~/.bitrise-xcelerate.
func (p Paths) XcelerateRoot() string {
	return filepath.Join(p.Home, XcelerateRootRelative)
}

// XcelerateConfigFile returns ~/.bitrise-xcelerate/config.json.
func (p Paths) XcelerateConfigFile() string {
	return filepath.Join(p.XcelerateRoot(), xcelerateConfigFile)
}

// XcelerateBinDir returns ~/.bitrise-xcelerate/bin.
func (p Paths) XcelerateBinDir() string {
	return filepath.Join(p.XcelerateRoot(), xcelerateBinSubdir)
}

// XcelerateBinFile returns a file path under XcelerateBinDir.
func (p Paths) XcelerateBinFile(name string) string {
	return filepath.Join(p.XcelerateBinDir(), name)
}

// ProxySocketPath returns the xcelerate proxy unix-socket path under the supplied temp dir.
func (p Paths) ProxySocketPath(tempDir string) string {
	return filepath.Join(tempDir, ProxySocketName)
}

// ProxyPidFile returns the xcelerate proxy pid file path under XcelerateRoot.
func (p Paths) ProxyPidFile() string {
	return filepath.Join(p.XcelerateRoot(), ProxyPidFileName)
}

// CcacheSocketPath returns the ccache IPC unix-socket path under the supplied temp dir.
func (p Paths) CcacheSocketPath(tempDir string) string {
	return filepath.Join(tempDir, CcacheSocketName)
}

// XcelerateLogDir returns ~/.local/state/xcelerate/logs.
func (p Paths) XcelerateLogDir() string {
	return filepath.Join(p.Home, xcelerateLogsRelative)
}

// CcacheLogDir returns ~/.local/state/ccache/logs.
func (p Paths) CcacheLogDir() string {
	return filepath.Join(p.Home, ccacheLogsRelative)
}

// GradleInitScriptFile returns the absolute path of the generated gradle init script.
func (p Paths) GradleInitScriptFile() string {
	return filepath.Join(p.Home, gradleInitScriptRelative)
}
