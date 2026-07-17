//go:build unit

package xcode_app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderOverride_writesRemoteServicePathAndKnownCacheKeys(t *testing.T) {
	got, err := RenderOverride("/tmp/xcelerate-proxy.sock", "", "/Users/alice")
	require.NoError(t, err)

	assert.Contains(t, got, "COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock")
	assert.Contains(t, got, "COMPILATION_CACHE_ENABLE_PLUGIN = YES")
	assert.Contains(t, got, "COMPILATION_CACHE_ENABLE_INTEGRATED_QUERIES = YES")
	assert.Contains(t, got, "COMPILATION_CACHE_ENABLE_DETACHED_KEY_QUERIES = YES")
	assert.Contains(t, got, "SWIFT_ENABLE_COMPILE_CACHE = YES")
	assert.Contains(t, got, "CLANG_ENABLE_COMPILE_CACHE = YES")
	assert.Contains(t, got, "COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES = swift c c++ objective-c objective-c++")
	assert.Contains(t, got, "CLANG_ENABLE_PREFIX_MAPPING = YES")
	assert.Contains(t, got, "SWIFT_ENABLE_PREFIX_MAPPING = YES")
	assert.Contains(t, got, "COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS = NO")
	assert.Contains(t, got, "OTHER_CFLAGS = $(inherited) -fdepscan-prefix-map=/Users/alice=/^home")
}

func TestRenderOverride_emptyHomeDirIsError(t *testing.T) {
	_, err := RenderOverride("/tmp/p.sock", "", "")
	require.Error(t, err)
}

func TestRenderOverride_includesPreviousFileBeforeKeys(t *testing.T) {
	got, err := RenderOverride("/tmp/p.sock", "/Users/me/MyProject.xcconfig", "/Users/alice")
	require.NoError(t, err)

	idxInclude := strings.Index(got, `#include "/Users/me/MyProject.xcconfig"`)
	idxRemote := strings.Index(got, "COMPILATION_CACHE_REMOTE_SERVICE_PATH")

	require.GreaterOrEqual(t, idxInclude, 0, "expected an #include line for the previous xcconfig")
	require.Greater(t, idxRemote, idxInclude, "expected our keys to follow the #include")
}

func TestRenderOverride_noIncludeWhenNoPrevious(t *testing.T) {
	got, err := RenderOverride("/tmp/p.sock", "", "/Users/alice")
	require.NoError(t, err)

	assert.NotContains(t, got, "#include")
}

func TestRenderOverride_emptyProxySocketIsError(t *testing.T) {
	_, err := RenderOverride("", "", "/Users/alice")
	require.Error(t, err)
}

func TestRenderOverride_includePathWithSpacesIsQuoted(t *testing.T) {
	got, err := RenderOverride("/tmp/p.sock", "/Users/me/With Space/Base.xcconfig", "/Users/alice")
	require.NoError(t, err)

	assert.Contains(t, got, `#include "/Users/me/With Space/Base.xcconfig"`)
}

func TestRenderOverride_rejectsIncludePathWithQuote(t *testing.T) {
	_, err := RenderOverride("/tmp/p.sock", `/Users/me/"weird".xcconfig`, "/Users/alice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quote")
}
