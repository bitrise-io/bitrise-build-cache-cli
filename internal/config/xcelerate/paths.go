package xcelerate

import (
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const (
	BinDir              = "bin"
	ErrFmtDetermineHome = `could not determine home: %w`
)

// DirPath returns the xcelerate root dir (~/.bitrise-xcelerate) using osProxy's home;
// falls back to working dir / executable dir / "." when home cannot be resolved.
func DirPath(osProxy utils.OsProxy) string {
	if home, err := osProxy.UserHomeDir(); err == nil {
		return paths.FromHome(home).XcelerateRoot()
	}

	if wd, err := osProxy.Getwd(); err == nil {
		return filepath.Join(wd, paths.XcelerateRootRelative)
	}

	if exe, err := osProxy.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return filepath.Join(dir, paths.XcelerateRootRelative)
		}
	}

	return filepath.Join(".", paths.XcelerateRootRelative)
}

func PathFor(osProxy utils.OsProxy, subpath string) string {
	return filepath.Join(DirPath(osProxy), subpath)
}

// ConfigFile returns the absolute path of the xcelerate config.json.
func ConfigFile(osProxy utils.OsProxy) string {
	return PathFor(osProxy, xcelerateConfigFileName)
}

// EnvProxySocketPath overrides the default xcelerate proxy socket location when set.
const EnvProxySocketPath = "BITRISE_XCELERATE_PROXY_SOCKET_PATH"

// EnvDerivedDataPath is exported by `activate xcode`; builds live at <root>/<workspace-sha>.
// Advisory only — the wrapper computes its own paths rather than reading it back.
const EnvDerivedDataPath = "BITRISE_XCODE_DERIVED_DATA_PATH"

// EnvInactivityTimeout overrides the xcelerate proxy inactivity window that
// triggers a slim invocation emit. Value is a time.ParseDuration string.
// Test-only knob: sole caller is the e2e-xcode-wrapper-watcher-race workflow, which
// forces slim-emit slow so the watcher's poll observes the manifest first.
const EnvInactivityTimeout = "TEST_BITRISE_XCELERATE_INACTIVITY_TIMEOUT"

// ResolveProxySocketPath returns the proxy unix socket path in the same order
// activate uses: explicit override → BITRISE_XCELERATE_PROXY_SOCKET_PATH env var
// → <temp-dir>/xcelerate-proxy.sock.
func ResolveProxySocketPath(override string, envs map[string]string, osProxy utils.OsProxy) string {
	if override != "" {
		return override
	}
	if env := envs[EnvProxySocketPath]; env != "" {
		return env
	}

	return paths.FromHome("").ProxySocketPath(osProxy.TempDir())
}

// EnvXcodeAppOverrideXCConfigPath overrides the on-disk location of the
// `xcode-app enable` override xcconfig. Callers rarely need it; kept so the
// doctor and the enable/disable commands read exactly the same value.
const EnvXcodeAppOverrideXCConfigPath = "BITRISE_XCODE_APP_OVERRIDE_XCCONFIG_PATH"

// ResolveXcodeAppOverrideXCConfigPath returns the override xcconfig path in the
// same order enable/disable and doctor use: explicit override → env var → the
// default ~/.bitrise-xcelerate/xcode-app.xcconfig. Falls back to a working-dir
// relative path when the home dir cannot be resolved, matching DirPath.
func ResolveXcodeAppOverrideXCConfigPath(override string, envs map[string]string, osProxy utils.OsProxy) string {
	if override != "" {
		return override
	}
	if env := envs[EnvXcodeAppOverrideXCConfigPath]; env != "" {
		return env
	}

	return filepath.Join(DirPath(osProxy), paths.XcodeAppOverrideXCConfigFileName)
}
