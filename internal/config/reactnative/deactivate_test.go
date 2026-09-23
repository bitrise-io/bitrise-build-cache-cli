//go:build unit

package reactnative_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/reactnative"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func TestDeactivate_ReactNative_RemovesMarker(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dir := paths.FromHome(tmpHome).BitriseCacheDir("reactnative")
	marker := filepath.Join(dir, "config.json")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(marker, []byte(`{"enabled": true}`), 0o644))

	require.NoError(t, reactnative.Deactivate(mockLogger, reactnative.DeactivateParams{}))

	_, err := os.Stat(marker)
	assert.True(t, os.IsNotExist(err))
}

func TestDeactivate_ReactNative_Idempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	require.NoError(t, reactnative.Deactivate(mockLogger, reactnative.DeactivateParams{}))
	require.NoError(t, reactnative.Deactivate(mockLogger, reactnative.DeactivateParams{}))
}

func TestDeactivate_ReactNative_DryRunPreservesFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dir := paths.FromHome(tmpHome).BitriseCacheDir("reactnative")
	marker := filepath.Join(dir, "config.json")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := []byte(`{"enabled": true}`)
	require.NoError(t, os.WriteFile(marker, body, 0o644))

	require.NoError(t, reactnative.Deactivate(mockLogger, reactnative.DeactivateParams{DryRun: true}))

	got, err := os.ReadFile(marker)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}
