//go:build unit

package xcode_app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func TestRender_containsAllRequiredKeys(t *testing.T) {
	got, err := Render("/tmp/xcelerate-proxy.sock", "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib", true)
	require.NoError(t, err)

	for _, want := range []string{
		"CLANG_ENABLE_COMPILE_CACHE = YES",
		"CLANG_ENABLE_MODULES = YES",
		"COMPILATION_CACHE_ENABLE_CACHING = YES",
		"COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS = YES",
		"COMPILATION_CACHE_ENABLE_PLUGIN = YES",
		"COMPILATION_CACHE_PLUGIN_PATH = /var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib",
		"COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock",
		"SWIFT_ENABLE_COMPILE_CACHE = YES",
		"TOOLCHAINS = " + ToolchainID,
	} {
		assert.Contains(t, got, want)
	}
}

func TestRender_omitsToolchainsWhenNotInstalled(t *testing.T) {
	got, err := Render("/tmp/x.sock", "/dev/null", false)
	require.NoError(t, err)

	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "//") {
			continue
		}
		assert.NotContains(t, line, "TOOLCHAINS",
			"missing toolchain → TOOLCHAINS setting must be omitted (comment is OK)")
	}
	assert.Contains(t, got, "SWIFT_ENABLE_COMPILE_CACHE = YES",
		"other settings must still be emitted")
}

func TestRender_omitsRemoteSupportedLanguages(t *testing.T) {
	got, err := Render("/tmp/x.sock", "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib", true)
	require.NoError(t, err)

	assert.NotContains(t, got, "COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES")
}

func TestRender_emptyProxySocketIsError(t *testing.T) {
	_, err := Render("", "/dev/null", true)
	require.Error(t, err)
}

func TestRender_emptyPluginPathIsError(t *testing.T) {
	_, err := Render("/tmp/x.sock", "", true)
	require.Error(t, err)
}

func TestRender_honorsCASPluginPathOverride(t *testing.T) {
	got, err := Render("/tmp/x.sock", "/opt/custom/libToolchainCASPlugin.dylib", true)
	require.NoError(t, err)

	assert.Contains(t, got, "COMPILATION_CACHE_PLUGIN_PATH = /opt/custom/libToolchainCASPlugin.dylib")
}

const testPluginPath = "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib"

func TestWriteOverrideXCConfig_happyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, WriteOverrideXCConfig(utils.DefaultOsProxy{}, nil, "/tmp/xcelerate-proxy.sock", testPluginPath))

	path := filepath.Join(home, ".bitrise-xcelerate", "xcode-app.xcconfig")
	body, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(body), "COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock")
}

func TestWriteOverrideXCConfig_honorsCASPluginPathArg(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, WriteOverrideXCConfig(utils.DefaultOsProxy{}, nil, "/tmp/xcelerate-proxy.sock", "/opt/custom/libToolchainCASPlugin.dylib"))

	path := filepath.Join(home, ".bitrise-xcelerate", "xcode-app.xcconfig")
	body, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(body), "COMPILATION_CACHE_PLUGIN_PATH = /opt/custom/libToolchainCASPlugin.dylib")
}

func TestWriteOverrideXCConfig_honorsEnvOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	overrideDir := t.TempDir()
	overridePath := filepath.Join(overrideDir, "custom", "xcode-app.xcconfig")
	envs := map[string]string{EnvOverrideXCConfigPath: overridePath}

	require.NoError(t, WriteOverrideXCConfig(utils.DefaultOsProxy{}, envs, "/tmp/xcelerate-proxy.sock", testPluginPath))

	body, err := os.ReadFile(overridePath) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(body), "COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock")

	// Writer and reader must agree on the resolved path.
	defaultPath := filepath.Join(home, ".bitrise-xcelerate", "xcode-app.xcconfig")
	_, err = os.Stat(defaultPath)
	assert.True(t, os.IsNotExist(err), "default path should not be written when env override is set, got err=%v", err)
}

// TestWriteOverrideXCConfig_SkipsIncludeWhenToolchainMissing pins the probe:
// a happy-path activate on a host where the toolchain install has not run (or
// was removed) must still produce a usable xcconfig, with the `TOOLCHAINS`
// line omitted so Xcode does not fail to resolve a missing bundle.
func TestWriteOverrideXCConfig_SkipsIncludeWhenToolchainMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, WriteOverrideXCConfig(utils.DefaultOsProxy{}, nil, "/tmp/xcelerate-proxy.sock", testPluginPath))

	path := filepath.Join(home, ".bitrise-xcelerate", "xcode-app.xcconfig")
	body, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "//") {
			continue
		}
		assert.NotContains(t, line, "TOOLCHAINS",
			"missing toolchain → TOOLCHAINS setting must be omitted (comment is OK)")
	}
	assert.Contains(t, string(body), "COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock",
		"rest of the xcconfig must still be emitted")
}

// TestWriteOverrideXCConfig_IncludesToolchainWhenInstalled pins the companion
// positive case: with the install path symlinked + ToolchainInfo.plist on
// disk, the writer emits `TOOLCHAINS = <id>`.
func TestWriteOverrideXCConfig_IncludesToolchainWhenInstalled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks under HOME require elevated perms on Windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	installPath := filepath.Join(home, ".bitrise-xcelerate", "toolchain", "io.bitrise.cas.xctoolchain")
	require.NoError(t, os.MkdirAll(installPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installPath, "ToolchainInfo.plist"), []byte("<plist/>"), 0o644))

	linkPath := filepath.Join(home, "Library", "Developer", "Toolchains", "io.bitrise.cas.xctoolchain")
	require.NoError(t, os.MkdirAll(filepath.Dir(linkPath), 0o755))
	require.NoError(t, os.Symlink(installPath, linkPath))

	require.NoError(t, WriteOverrideXCConfig(utils.DefaultOsProxy{}, nil, "/tmp/xcelerate-proxy.sock", testPluginPath))

	path := filepath.Join(home, ".bitrise-xcelerate", "xcode-app.xcconfig")
	body, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(body), "TOOLCHAINS = "+ToolchainID)
}

func TestWriteOverrideXCConfig_mkdirErrorPropagates(t *testing.T) {
	home := t.TempDir()

	osProxy := &mocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		MkdirAllFunc:    func(string, os.FileMode) error { return &fs.PathError{Op: "mkdir", Path: home, Err: errors.New("boom")} },
	}

	err := WriteOverrideXCConfig(osProxy, nil, "/tmp/x.sock", testPluginPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir")
}
