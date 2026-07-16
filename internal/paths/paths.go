// Package paths centralises the on-disk locations the CLI reads and writes.
// One package so the layout under ~/.local/state/bitrise-build-cache stays
// consistent across versioncheck, refresh, daemon supervisor logs, and the
// future Xcelerate / ccache state dirs.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
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

	// xcelerateStateRelative is the per-user xcelerate state root.
	xcelerateStateRelative = ".local/state/xcelerate"

	// xcelerateLogsSubdir is the per-user xcelerate log dir under XcelerateStateDir.
	xcelerateLogsSubdir = "logs"

	// xcelerateEnrichmentSubdir holds every persisted-state artefact the
	// F2 enrichment watcher, retry queue, and slim/handled-marker bookkeeping share.
	xcelerateEnrichmentSubdir = "enrichment"

	// xcelerateHandledInvocationsSubdir sits under XcelerateEnrichmentDir and marks
	// invocation IDs the wrapper already PUT a rich payload for, so slim emit and
	// F2 enrichment skip them instead of last-write-wins overwriting the rich row.
	xcelerateHandledInvocationsSubdir = "handled-invocations"

	// handledManifestsFilename is the NDJSON append-only log of xcactivitylog UUIDs
	// the Watcher has already emitted, so a proxy restart doesn't replay historic manifests.
	handledManifestsFilename = "handled-manifests.ndjson"

	// ccacheLogsRelative is the per-user ccache log dir.
	ccacheLogsRelative = ".local/state/ccache/logs"

	// daemonLogsSubdir is the daemon supervisor stdout/stderr log dir.
	daemonLogsSubdir = "logs"

	// invocationsSubdir holds the per-day NDJSON invocation log files.
	invocationsSubdir = "invocations"

	pendingInvocationsFilename = "pending-invocations.ndjson"

	enrichmentHealthFilename = "health.json"

	// bitriseBinSubdir holds the stable CLI binary copy used by the daemon supervisor.
	bitriseBinSubdir = "bin"

	// bitriseCacheSubdir is the per-tool cache/marker root used by activate, refresh, and child-stats.
	bitriseCacheSubdir = "cache"

	// xcelerateBinSubdir holds the xcelerate wrapper scripts (xcodebuild / xcrun) and CLI copy.
	xcelerateBinSubdir = "bin"

	// xcelerateConfigFile is the JSON config file written by `activate xcode`.
	xcelerateConfigFile = "config.json"

	// XcodeAppOverrideXCConfigFileName is the override xcconfig written by `xcode-app enable`.
	XcodeAppOverrideXCConfigFileName = "xcode-app.xcconfig"

	// XcodeAppBridgeXCConfigName is the sibling xcconfig written next to a
	// .xcodeproj by `xcode-app link`. It contains a single `#include` pointing
	// at the user-global override xcconfig managed by `xcode-app enable`.
	//
	// The name is intentionally NOT dot-prefixed — Xcode's base-configuration
	// file picker hides dotfiles.
	XcodeAppBridgeXCConfigName = "bitrise-build-cache-xcode.xcconfig"

	// XcodeAppSetenvAgentLabel is the launchd label + plist basename for the xcode-app setenv agent.
	XcodeAppSetenvAgentLabel = "io.bitrise.build-cache.xcode-app-setenv"

	// XcodeAppStateFileName holds the prior XCODE_XCCONFIG_FILE captured at enable time.
	XcodeAppStateFileName = "xcode-app-state.json"

	// linkedProjectsSubdir sits under XcelerateRoot and holds per-project link-state
	// JSON files (one per .xcodeproj that `xcode-app link` has touched), so `unlink`
	// can revert the exact set of xcconfig files it appended trailers to.
	linkedProjectsSubdir = "linked-projects"

	// xcodeManagedDerivedDataTool is the per-workspace DD root managed by the wrapper.
	xcodeManagedDerivedDataTool = "xcode-dd"

	// xcodeManagedProjectTempDirTool is the per-workspace PROJECT_TEMP_DIR root managed by the wrapper.
	xcodeManagedProjectTempDirTool = "xcode-ptd"

	// gradleInitScriptRelative is the per-user gradle init script written by `activate gradle`.
	gradleInitScriptRelative = ".gradle/init.d/bitrise-build-cache.init.gradle.kts"

	// XcodeManagedDerivedDataManifestGlobRelative is the HOME-relative glob matching
	// LogStoreManifest.plist under every wrapper-owned DerivedData workspace-sha.
	XcodeManagedDerivedDataManifestGlobRelative = BitriseRootRelative + "/" + bitriseCacheSubdir + "/" + xcodeManagedDerivedDataTool + "/*/Logs/*/LogStoreManifest.plist"
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

func (p Paths) PendingInvocationsFile() string {
	return filepath.Join(p.XcelerateEnrichmentDir(), pendingInvocationsFilename)
}

func (p Paths) EnrichmentHealthFile() string {
	return filepath.Join(p.XcelerateEnrichmentDir(), enrichmentHealthFilename)
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

// XcodeAppOverrideXCConfigFile returns ~/.bitrise-xcelerate/xcode-app.xcconfig.
func (p Paths) XcodeAppOverrideXCConfigFile() string {
	return filepath.Join(p.XcelerateRoot(), XcodeAppOverrideXCConfigFileName)
}

// XcodeAppStateFile returns ~/.bitrise-xcelerate/xcode-app-state.json.
func (p Paths) XcodeAppStateFile() string {
	return filepath.Join(p.XcelerateRoot(), XcodeAppStateFileName)
}

// XcodeAppSetenvAgentPlistFile returns the LaunchAgent plist path for the xcode-app setenv agent.
func (p Paths) XcodeAppSetenvAgentPlistFile() string {
	return p.PlistPath(XcodeAppSetenvAgentLabel)
}

// LinkedProjectsDir returns ~/.bitrise-xcelerate/linked-projects.
func (p Paths) LinkedProjectsDir() string {
	return filepath.Join(p.XcelerateRoot(), linkedProjectsSubdir)
}

// LinkedProjectStateFilename returns just the "<hex>.state.json" basename for
// a project path. Same input → same filename; different absolute paths hash to
// distinct files so two projects sharing a basename stay isolated.
// Kept separate from LinkedProjectStateFile so callers (e.g. the xcode_app
// package's own in-memory tests) can derive filenames without a Paths root.
func LinkedProjectStateFilename(projectPath string) string {
	sum := sha256.Sum256([]byte(projectPath))

	return hex.EncodeToString(sum[:8]) + ".state.json"
}

// LinkedProjectStateFile returns the state file path for a project, joining
// LinkedProjectStateFilename onto LinkedProjectsDir.
func (p Paths) LinkedProjectStateFile(projectPath string) string {
	return filepath.Join(p.LinkedProjectsDir(), LinkedProjectStateFilename(projectPath))
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

// CcacheSocketPath returns the ccache IPC unix-socket path under the supplied temp dir.
func (p Paths) CcacheSocketPath(tempDir string) string {
	return filepath.Join(tempDir, CcacheSocketName)
}

// XcelerateStateDir returns ~/.local/state/xcelerate.
func (p Paths) XcelerateStateDir() string {
	return filepath.Join(p.Home, xcelerateStateRelative)
}

// XcelerateLogDir returns ~/.local/state/xcelerate/logs.
func (p Paths) XcelerateLogDir() string {
	return filepath.Join(p.XcelerateStateDir(), xcelerateLogsSubdir)
}

// XcelerateHandledInvocationDir returns ~/.local/state/xcelerate/enrichment/handled-invocations.
func (p Paths) XcelerateHandledInvocationDir() string {
	return filepath.Join(p.XcelerateEnrichmentDir(), xcelerateHandledInvocationsSubdir)
}

// XcelerateHandledInvocationFile returns the marker path for a specific invocation ID.
func (p Paths) XcelerateHandledInvocationFile(invocationID string) string {
	return filepath.Join(p.XcelerateHandledInvocationDir(), invocationID)
}

// XcelerateEnrichmentDir returns ~/.local/state/xcelerate/enrichment.
func (p Paths) XcelerateEnrichmentDir() string {
	return filepath.Join(p.XcelerateStateDir(), xcelerateEnrichmentSubdir)
}

// HandledManifestsFile returns the NDJSON log the enrichment Watcher uses to
// persist which xcactivitylog UUIDs have already been emitted across restarts.
func (p Paths) HandledManifestsFile() string {
	return filepath.Join(p.XcelerateEnrichmentDir(), handledManifestsFilename)
}

// CcacheLogDir returns ~/.local/state/ccache/logs.
func (p Paths) CcacheLogDir() string {
	return filepath.Join(p.Home, ccacheLogsRelative)
}

// GradleInitScriptFile returns the absolute path of the generated gradle init script.
func (p Paths) GradleInitScriptFile() string {
	return filepath.Join(p.Home, gradleInitScriptRelative)
}

// XcodeManagedDerivedDataDir returns the wrapper-owned DerivedData dir for a given
// workspace-sha, layered under BitriseCacheDir("xcode-dd").
func (p Paths) XcodeManagedDerivedDataDir(workspaceSHA string) string {
	return filepath.Join(p.BitriseCacheDir(xcodeManagedDerivedDataTool), workspaceSHA)
}

// XcodeManagedProjectTempDir returns the wrapper-owned PROJECT_TEMP_DIR dir for a given
// workspace-sha, layered under BitriseCacheDir("xcode-ptd").
func (p Paths) XcodeManagedProjectTempDir(workspaceSHA string) string {
	return filepath.Join(p.BitriseCacheDir(xcodeManagedProjectTempDirTool), workspaceSHA)
}
