//go:build unit

package proxy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
)

func TestNewBuildRequestInfoLoader_ReadsAndCaches(t *testing.T) {
	dd := t.TempDir()
	hex := "a1b2c3d4e5f6071829304152637485a0"
	dir := filepath.Join(dd, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"),
		[]byte(`{"parameters":{"action":"build","qos":"default"}}`), 0o644))

	loader := proxy.NewBuildRequestInfoLoader(nil)
	action, qos := loader(dd, hex)
	assert.Equal(t, "build", action)
	assert.Equal(t, "default", qos)

	// Second call must hit the cache — mutate the file and confirm the loader
	// still returns the original tuple.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"),
		[]byte(`{"parameters":{"action":"archive","qos":""}}`), 0o644))
	action, qos = loader(dd, hex)
	assert.Equal(t, "build", action, "cache must not re-read disk on repeat (DD,hex)")
	assert.Equal(t, "default", qos)
}

func TestNewBuildRequestInfoLoader_MissingReturnsEmptyAndCaches(t *testing.T) {
	dd := t.TempDir()
	hex := "ffffffffffffffffffffffffffffffff"

	loader := proxy.NewBuildRequestInfoLoader(nil)
	action, qos := loader(dd, hex)
	assert.Empty(t, action)
	assert.Empty(t, qos)

	// Second call still returns empty via cache.
	action, qos = loader(dd, hex)
	assert.Empty(t, action)
	assert.Empty(t, qos)
}

func TestNewBuildRequestInfoLoader_EmptyInputsReturnEmpty(t *testing.T) {
	loader := proxy.NewBuildRequestInfoLoader(nil)

	action, qos := loader("", "hex")
	assert.Empty(t, action)
	assert.Empty(t, qos)

	action, qos = loader("/tmp", "")
	assert.Empty(t, action)
	assert.Empty(t, qos)
}
