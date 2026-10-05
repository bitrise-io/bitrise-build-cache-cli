//go:build unit

package xcode_app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func TestRender_containsAllRequiredKeys(t *testing.T) {
	got, err := Render("/tmp/xcelerate-proxy.sock", "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib")
	require.NoError(t, err)

	for _, want := range []string{
		"CLANG_ENABLE_COMPILE_CACHE = YES",
		"CLANG_ENABLE_MODULES = YES",
		"COMPILATION_CACHE_ENABLE_CACHING = YES",
		"COMPILATION_CACHE_ENABLE_PLUGIN = YES",
		"COMPILATION_CACHE_PLUGIN_PATH = /var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib",
		"COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock",
		"SWIFT_ENABLE_COMPILE_CACHE = YES",
		"TOOLCHAINS = " + ToolchainID,
	} {
		assert.Contains(t, got, want)
	}
}

func TestRender_omitsRemoteSupportedLanguages(t *testing.T) {
	got, err := Render("/tmp/x.sock", "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib")
	require.NoError(t, err)

	assert.NotContains(t, got, "COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES")
}

func TestRender_emptyProxySocketIsError(t *testing.T) {
	_, err := Render("", "/dev/null")
	require.Error(t, err)
}

func TestRender_emptyPluginPathIsError(t *testing.T) {
	_, err := Render("/tmp/x.sock", "")
	require.Error(t, err)
}

func TestRender_honorsCASPluginPathOverride(t *testing.T) {
	got, err := Render("/tmp/x.sock", "/opt/custom/libToolchainCASPlugin.dylib")
	require.NoError(t, err)

	assert.Contains(t, got, "COMPILATION_CACHE_PLUGIN_PATH = /opt/custom/libToolchainCASPlugin.dylib")
}

func TestResolveCASPluginPath_envWins(t *testing.T) {
	got := ResolveCASPluginPath(map[string]string{EnvCASPluginPath: "/opt/custom/libToolchainCASPlugin.dylib"})
	assert.Equal(t, "/opt/custom/libToolchainCASPlugin.dylib", got)
}

func TestResolveCASPluginPath_defaultAnchorRooted(t *testing.T) {
	got := ResolveCASPluginPath(nil)
	assert.Equal(t, "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib", got)
}

func TestWriteOverrideXCConfig_happyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, WriteOverrideXCConfig(utils.DefaultOsProxy{}, nil, "/tmp/xcelerate-proxy.sock"))

	path := filepath.Join(home, ".bitrise-xcelerate", "xcode-app.xcconfig")
	body, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(body), "COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock")
}

func TestWriteOverrideXCConfig_honorsCASPluginPathEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	envs := map[string]string{EnvCASPluginPath: "/opt/custom/libToolchainCASPlugin.dylib"}
	require.NoError(t, WriteOverrideXCConfig(utils.DefaultOsProxy{}, envs, "/tmp/xcelerate-proxy.sock"))

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

	require.NoError(t, WriteOverrideXCConfig(utils.DefaultOsProxy{}, envs, "/tmp/xcelerate-proxy.sock"))

	body, err := os.ReadFile(overridePath) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(body), "COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock")

	// Writer and reader must agree on the resolved path.
	defaultPath := filepath.Join(home, ".bitrise-xcelerate", "xcode-app.xcconfig")
	_, err = os.Stat(defaultPath)
	assert.True(t, os.IsNotExist(err), "default path should not be written when env override is set, got err=%v", err)
}

func TestWriteOverrideXCConfig_mkdirErrorPropagates(t *testing.T) {
	home := t.TempDir()

	osProxy := &mocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		MkdirAllFunc:    func(string, os.FileMode) error { return &fs.PathError{Op: "mkdir", Path: home, Err: errors.New("boom")} },
	}

	err := WriteOverrideXCConfig(osProxy, nil, "/tmp/x.sock")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir")
}
