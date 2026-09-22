//go:build unit

package common

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func TestPersistProjectMode_EmptyFlagIsNoop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, PersistProjectMode("", nil))

	_, statErr := os.Stat(paths.FromHome(home).MachineConfigFile())
	assert.True(t, os.IsNotExist(statErr), "empty flag must not create machine config")
}

func TestPersistProjectMode_WritesFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, PersistProjectMode(string(machineconfig.ModeOptIn), nil))

	cfg, err := machineconfig.Read(utils.DefaultOsProxy{}, paths.FromHome(home), nil)
	require.NoError(t, err)
	assert.Equal(t, machineconfig.ModeOptIn, cfg.ProjectMode)
}

func TestPersistProjectMode_InvalidFlagReturnsError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	err := PersistProjectMode("garbage", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "garbage")
}
