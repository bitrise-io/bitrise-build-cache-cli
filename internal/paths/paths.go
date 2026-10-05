// Package paths centralises the on-disk locations the CLI reads and writes.
// One package so the layout under ~/.local/state/bitrise-build-cache stays
// consistent across versioncheck, refresh, and the
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

	// BitriseBuildCacheDirRelative is the repo-local config dir committed alongside the source
	// tree, holding files such as the persisted xcode-{build,test}.json invocation specs.
	BitriseBuildCacheDirRelative = ".bitrise-build-cache"

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
	// enrichment watcher and retry queue share.
	xcelerateEnrichmentSubdir = "enrichment"

	// xcelerateDsymShimSubdir holds the per-invocation touch files the
	// dsymutil CAS shim drops; the xcodebuild wrapper drains them at
	// end-of-run and folds the counts into analytics.
	xcelerateDsymShimSubdir = "dsymshim"

	// handledManifestsFilename is the NDJSON append-only log of xcactivitylog UUIDs
	// the Watcher has already emitted, so a proxy restart doesn't replay historic manifests.
	handledManifestsFilename = "handled-manifests.ndjson"

	// ccacheLogsRelative is the per-user ccache log dir.
	ccacheLogsRelative = ".local/state/ccache/logs"

	// invocationsSubdir holds the per-day NDJSON invocation log files.
	invocationsSubdir = "invocations"

	pendingInvocationsFilename = "pending-invocations.ndjson"

	enrichmentHealthFilename = "health.json"

	authRefreshLockFilename = "auth-refresh.lock"

	bazelCredHelperWarnFilename = "bazel-credhelper-warned" //nolint:gosec // marker filename, not a credential

	// bitriseCacheSubdir is the per-tool cache/marker root used by activate, refresh, and child-stats.
	bitriseCacheSubdir = "cache"

	// xcelerateBinSubdir holds the xcelerate wrapper scripts (xcodebuild / xcrun) and CLI copy.
	xcelerateBinSubdir = "bin"

	// xcelerateToolchainsSubdir holds staged custom Xcode toolchains the CLI installs
	// (currently just the dsymutil-CAS-shim farm).
	xcelerateToolchainsSubdir = "toolchains"

	// DsymutilCasShimToolchainBundleName is the on-disk directory name of the staged custom
	// toolchain. It is also the CFBundleIdentifier used in TOOLCHAINS=<id> $(inherited).
	DsymutilCasShimToolchainBundleName = "com.bitrise.cas-shim.xctoolchain"

	// DsymutilCasShimToolchainID is the toolchain identifier passed to xcodebuild via the
	// TOOLCHAINS build setting; matches the farm's Info.plist CFBundleIdentifier.
	DsymutilCasShimToolchainID = "com.bitrise.cas-shim"

	// XcodeUserToolchainsDirRelative is Apple's per-user Toolchains dir scanned by xcodebuild
	// when resolving TOOLCHAINS=<id>; relative to $HOME.
	XcodeUserToolchainsDirRelative = "Library/Developer/Toolchains"

	// CompilationCachePluginDirRelative is the plugin store the Apple compile-cache plugin
	// writes under either DerivedData or PROJECT_TEMP_DIR. The shim reads it to pass -cas <dir>
	// to dsymutil.
	CompilationCachePluginDirRelative = "CompilationCache.noindex/plugin"

	// xcelerateConfigFile is the JSON config file written by `activate xcode`.
	xcelerateConfigFile = "config.json"

	// xcodeManagedDerivedDataTool is the per-workspace DD root managed by the wrapper.
	xcodeManagedDerivedDataTool = "xcode-dd"

	// xcodeManagedProjectTempDirTool is the per-workspace PROJECT_TEMP_DIR root managed by the wrapper.
	xcodeManagedProjectTempDirTool = "xcode-ptd"

	// gradleInitScriptRelative is the per-user gradle init script written by `activate gradle`.
	gradleInitScriptRelative = ".gradle/init.d/bitrise-build-cache.init.gradle.kts"

	// ProjectMarkerFilename is the per-project opt-in file consulted by every tool activator.
	ProjectMarkerFilename = ".bitrise-build-cache.json"

	buildCacheMachineConfigFilename = "config.json"

	// XcodeManagedDerivedDataManifestGlobRelative is the HOME-relative glob matching
	// LogStoreManifest.plist under every wrapper-owned DerivedData workspace-sha.
	XcodeManagedDerivedDataManifestGlobRelative = BitriseRootRelative + "/" + bitriseCacheSubdir + "/" + xcodeManagedDerivedDataTool + "/*/Logs/*/LogStoreManifest.plist"
)

// CLIBinaryName is the on-disk name of the CLI executable.
const CLIBinaryName = "bitrise-build-cache"

// XcelerateCLIBinaryName is hardcoded by the xcodebuild / xcrun wrapper scripts;
// kept distinct from CLIBinaryName so installer.sh and the xcelerate copy don't
// collide on PATH.
const XcelerateCLIBinaryName = "bitrise-build-cache-cli"

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

func (p Paths) AuthRefreshLockFile() string {
	return p.StateFile(authRefreshLockFilename)
}

func (p Paths) BazelCredHelperWarnFile() string {
	return p.StateFile(bazelCredHelperWarnFilename)
}

// LaunchAgentsDir is the absolute path of the per-user macOS LaunchAgents dir.
func (p Paths) LaunchAgentsDir() string {
	return filepath.Join(p.Home, LaunchAgentsDirRelative)
}

// SystemdUserDir is the absolute path of the per-user systemd unit dir.
func (p Paths) SystemdUserDir() string {
	return filepath.Join(p.Home, SystemdUserDirRelative)
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

// BitriseRoot is the absolute path of the per-user ~/.bitrise dir.
func (p Paths) BitriseRoot() string {
	return filepath.Join(p.Home, BitriseRootRelative)
}

// BitriseCacheRoot is the per-user cache root ~/.bitrise/cache.
func (p Paths) BitriseCacheRoot() string {
	return filepath.Join(p.BitriseRoot(), bitriseCacheSubdir)
}

// BitriseCacheDir is the per-tool cache/marker dir under ~/.bitrise/cache.
func (p Paths) BitriseCacheDir(tool string) string {
	return filepath.Join(p.BitriseCacheRoot(), tool)
}

// MachineConfigFile is the absolute path of the machine-wide build-cache config file.
func (p Paths) MachineConfigFile() string {
	return filepath.Join(p.BitriseCacheRoot(), buildCacheMachineConfigFilename)
}

func (p Paths) MachineConfigTempFile() string {
	return filepath.Join(p.BitriseCacheRoot(), "."+buildCacheMachineConfigFilename+".tmp")
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

// XcelerateToolchainsDir returns ~/.bitrise-xcelerate/toolchains, where custom Xcode
// toolchains the CLI stages live before being symlinked into Apple's scan paths.
func (p Paths) XcelerateToolchainsDir() string {
	return filepath.Join(p.XcelerateRoot(), xcelerateToolchainsSubdir)
}

// DsymutilCasShimToolchainStagingDir returns the on-disk toolchain-farm root the CLI
// stages, under ~/.bitrise-xcelerate/toolchains/.
func (p Paths) DsymutilCasShimToolchainStagingDir() string {
	return filepath.Join(p.XcelerateToolchainsDir(), DsymutilCasShimToolchainBundleName)
}

// DsymutilCasShimToolchainBinDir returns the usr/bin dir inside the staged toolchain farm,
// where the shim binary and symlinks to stock toolchain siblings live.
func (p Paths) DsymutilCasShimToolchainBinDir() string {
	return filepath.Join(p.DsymutilCasShimToolchainStagingDir(), "usr", "bin")
}

// DsymutilCasShimPath returns the on-disk path of the staged dsymutil shim.
func (p Paths) DsymutilCasShimPath() string {
	return filepath.Join(p.DsymutilCasShimToolchainBinDir(), "dsymutil")
}

// DsymutilCasShimToolchainStampFile returns the farm's version-stamp file. The CLI
// re-stages when the stamp disagrees with the current Xcode location + build number.
func (p Paths) DsymutilCasShimToolchainStampFile() string {
	return filepath.Join(p.DsymutilCasShimToolchainStagingDir(), "stamp.json")
}

// XcodeUserToolchainsDir returns Apple's ~/Library/Developer/Toolchains dir, which Xcode
// scans for TOOLCHAINS=<id> matches. The CLI symlinks its staged farm into this dir.
func (p Paths) XcodeUserToolchainsDir() string {
	return filepath.Join(p.Home, XcodeUserToolchainsDirRelative)
}

// DsymutilCasShimUserToolchainSymlink returns the symlink the CLI maintains inside
// Apple's user Toolchains dir, pointing at the staged farm.
func (p Paths) DsymutilCasShimUserToolchainSymlink() string {
	return filepath.Join(p.XcodeUserToolchainsDir(), DsymutilCasShimToolchainBundleName)
}

// XcelerateDsymShimDir returns ~/.local/state/xcelerate/dsymshim, the drop dir for the
// shim's per-invocation touch files; the xcodebuild wrapper drains it at end-of-run.
func (p Paths) XcelerateDsymShimDir() string {
	return filepath.Join(p.XcelerateStateDir(), xcelerateDsymShimSubdir)
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
	return filepath.Join(p.XcodeManagedDerivedDataRoot(), workspaceSHA)
}

// XcodeManagedDerivedDataRoot returns the parent of every per-workspace DerivedData dir.
func (p Paths) XcodeManagedDerivedDataRoot() string {
	return p.BitriseCacheDir(xcodeManagedDerivedDataTool)
}

// XcodeManagedProjectTempDir returns the wrapper-owned PROJECT_TEMP_DIR dir for a given
// workspace-sha, layered under BitriseCacheDir("xcode-ptd").
func (p Paths) XcodeManagedProjectTempDir(workspaceSHA string) string {
	return filepath.Join(p.BitriseCacheDir(xcodeManagedProjectTempDirTool), workspaceSHA)
}

// RepoLocalConfigPath returns <repoRoot>/.bitrise-build-cache/<filename>. Repo-rooted, not $HOME-rooted.
func RepoLocalConfigPath(repoRoot, filename string) string {
	return filepath.Join(repoRoot, BitriseBuildCacheDirRelative, filename)
}

// DirMaker is the subset of utils.OsProxy that EnsureDir needs.
type DirMaker interface {
	MkdirAll(path string, perm os.FileMode) error
}

// EnsureDir creates dir if it is missing. Activation uses it for the log dirs
// the tool's first run would otherwise create lazily, so a freshly activated
// setup doesn't report them as missing before that run.
func EnsureDir(m DirMaker, dir string) error {
	if err := m.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	return nil
}
