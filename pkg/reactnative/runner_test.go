//go:build unit

package reactnative

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func writeMultiplatformConfig(t *testing.T, home string, debug bool) {
	t.Helper()
	dir := filepath.Join(home, ".bitrise/analytics/multiplatform")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := `{"debugLogging":false}`
	if debug {
		body = `{"debugLogging":true}`
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644))
}

func TestResolveDebugLogging(t *testing.T) {
	osProxy := utils.DefaultOsProxy{}
	decoderFactory := utils.DefaultDecoderFactory{}

	t.Run("params on, disk off", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeMultiplatformConfig(t, home, false)

		assert.True(t, resolveDebugLogging(true, osProxy, decoderFactory), "params.DebugLogging must win when disk is off")
	})

	t.Run("params off, disk on", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeMultiplatformConfig(t, home, true)

		assert.True(t, resolveDebugLogging(false, osProxy, decoderFactory), "disk DebugLogging must win when params is off")
	})

	t.Run("both off", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeMultiplatformConfig(t, home, false)

		assert.False(t, resolveDebugLogging(false, osProxy, decoderFactory))
	})

	t.Run("params on, no disk config", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		assert.True(t, resolveDebugLogging(true, osProxy, decoderFactory), "missing disk config must not drop params.DebugLogging")
	})
}
