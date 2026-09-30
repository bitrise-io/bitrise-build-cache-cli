//go:build unit

package xcode_app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender_containsAllRequiredKeys(t *testing.T) {
	got, err := Render("/tmp/xcelerate-proxy.sock", "")
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
	got, err := Render("/tmp/x.sock", "")
	require.NoError(t, err)

	assert.NotContains(t, got, "COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES")
}

func TestRender_emptyProxySocketIsError(t *testing.T) {
	_, err := Render("", "")
	require.Error(t, err)
}

func TestRender_chainsPreviousIncludeBeforeKeys(t *testing.T) {
	got, err := Render("/tmp/x.sock", "/Users/me/Base.xcconfig")
	require.NoError(t, err)

	idxInclude := strings.Index(got, `#include? "/Users/me/Base.xcconfig"`)
	idxRemote := strings.Index(got, "COMPILATION_CACHE_REMOTE_SERVICE_PATH")

	require.GreaterOrEqual(t, idxInclude, 0, "expected an #include? line for the previous xcconfig")
	require.Greater(t, idxRemote, idxInclude, "expected our keys to follow the #include?")
}

func TestRender_noIncludeWhenPreviousEmpty(t *testing.T) {
	got, err := Render("/tmp/x.sock", "")
	require.NoError(t, err)

	assert.NotContains(t, got, "#include")
}

func TestRender_previousPathWithSpacesRoundTrips(t *testing.T) {
	got, err := Render("/tmp/x.sock", "/Users/me/With Space/Base.xcconfig")
	require.NoError(t, err)

	assert.Contains(t, got, `#include? "/Users/me/With Space/Base.xcconfig"`)
}

func TestRender_rejectsPreviousPathWithQuote(t *testing.T) {
	_, err := Render("/tmp/x.sock", `/Users/me/"weird".xcconfig`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quote")
}
