//go:build unit

package ccache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func newSilentCcacheTestLogger() log.Logger {
	m := &mocks.Logger{}
	m.On("Infof", mock.Anything).Return()
	m.On("Infof", mock.Anything, mock.Anything).Return()
	m.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	m.On("Debugf", mock.Anything).Return()
	m.On("Debugf", mock.Anything, mock.Anything).Return()
	m.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	m.On("TInfof", mock.Anything).Return()
	m.On("TInfof", mock.Anything, mock.Anything).Return()
	m.On("TInfof", mock.Anything, mock.Anything, mock.Anything).Return()

	return m
}

func TestDeactivate_Ccache_RemovesConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	configDir := paths.FromHome(tmpHome).BitriseCacheDir("ccache")
	configFile := filepath.Join(configDir, "config.json")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(configFile, []byte(`{"enabled": true}`), 0o644))

	logger := newSilentCcacheTestLogger()
	require.NoError(t, Deactivate(logger, DeactivateParams{}))

	_, err := os.Stat(configFile)
	assert.True(t, os.IsNotExist(err))
}

func TestDeactivate_Ccache_Idempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	logger := newSilentCcacheTestLogger()
	require.NoError(t, Deactivate(logger, DeactivateParams{}))
	require.NoError(t, Deactivate(logger, DeactivateParams{}))
}

func TestDeactivate_Ccache_DryRunPreservesFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	configDir := paths.FromHome(tmpHome).BitriseCacheDir("ccache")
	configFile := filepath.Join(configDir, "config.json")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	body := []byte(`{"enabled": true}`)
	require.NoError(t, os.WriteFile(configFile, body, 0o644))

	logger := newSilentCcacheTestLogger()
	require.NoError(t, Deactivate(logger, DeactivateParams{DryRun: true}))

	got, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}
