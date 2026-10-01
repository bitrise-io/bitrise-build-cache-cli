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
	got, err := Render("/tmp/xcelerate-proxy.sock")
	require.NoError(t, err)

	for _, want := range []string{
		"CLANG_ENABLE_COMPILE_CACHE = YES",
		"CLANG_ENABLE_MODULES = YES",
		"COMPILATION_CACHE_ENABLE_CACHING = YES",
		"COMPILATION_CACHE_ENABLE_PLUGIN = YES",
		"COMPILATION_CACHE_PLUGIN_PATH = " + AppleCASPluginPath,
		"COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock",
		"SWIFT_ENABLE_COMPILE_CACHE = YES",
		"OTHER_SWIFT_FLAGS = $(inherited) -cas-plugin-option remote-service-path=/tmp/xcelerate-proxy.sock",
	} {
		assert.Contains(t, got, want)
	}
}

func TestRender_omitsRemoteSupportedLanguages(t *testing.T) {
	got, err := Render("/tmp/x.sock")
	require.NoError(t, err)

	assert.NotContains(t, got, "COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES")
}

func TestRender_emptyProxySocketIsError(t *testing.T) {
	_, err := Render("")
	require.Error(t, err)
}

func TestWriteOverrideXCConfig_happyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, WriteOverrideXCConfig(utils.DefaultOsProxy{}, "/tmp/xcelerate-proxy.sock"))

	path := filepath.Join(home, ".bitrise-xcelerate", "xcode-app.xcconfig")
	body, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(body), "COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock")
}

func TestWriteOverrideXCConfig_mkdirErrorPropagates(t *testing.T) {
	home := t.TempDir()

	osProxy := &mocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		MkdirAllFunc:    func(string, os.FileMode) error { return &fs.PathError{Op: "mkdir", Path: home, Err: errors.New("boom")} },
	}

	err := WriteOverrideXCConfig(osProxy, "/tmp/x.sock")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir")
}
