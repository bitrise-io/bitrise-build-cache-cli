//go:build unit

package common

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func newCachePushCmd(t *testing.T, args []string) (*cobra.Command, bool) {
	t.Helper()

	var flagValue bool
	cmd := &cobra.Command{Use: "activate-something", Run: func(*cobra.Command, []string) {}}
	cmd.Flags().BoolVar(&flagValue, CachePushFlagName, true, "test flag")

	require.NoError(t, cmd.ParseFlags(args))

	return cmd, flagValue
}

func TestResolveAndPersistCachePush_FlagChangedTruePersistsAndReturns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd, flagValue := newCachePushCmd(t, []string{"--cache-push=true"})

	got, err := ResolveAndPersistCachePush(cmd, flagValue, nil)
	require.NoError(t, err)
	assert.True(t, got)

	cfg, err := machineconfig.Read(utils.DefaultOsProxy{}, paths.FromHome(home), nil)
	require.NoError(t, err)
	require.NotNil(t, cfg.CachePush, "flag changed should persist")
	assert.True(t, *cfg.CachePush)
}

func TestResolveAndPersistCachePush_FlagChangedFalsePersistsAndReturns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd, flagValue := newCachePushCmd(t, []string{"--cache-push=false"})

	got, err := ResolveAndPersistCachePush(cmd, flagValue, nil)
	require.NoError(t, err)
	assert.False(t, got)

	cfg, err := machineconfig.Read(utils.DefaultOsProxy{}, paths.FromHome(home), nil)
	require.NoError(t, err)
	require.NotNil(t, cfg.CachePush)
	assert.False(t, *cfg.CachePush)
}

func TestResolveAndPersistCachePush_FlagUnchangedDoesNotWriteFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd, flagValue := newCachePushCmd(t, []string{})

	got, err := ResolveAndPersistCachePush(cmd, flagValue, nil)
	require.NoError(t, err)
	assert.Equal(t, machineconfig.DefaultCachePush, got)

	_, statErr := os.Stat(paths.FromHome(home).MachineConfigFile())
	assert.True(t, os.IsNotExist(statErr), "no flag change must not create machine config")
}

func TestResolveAndPersistCachePush_FlagUnchangedReadsStoredValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	falseVal := false
	require.NoError(t, machineconfig.Write(machineconfig.Config{CachePush: &falseVal}, utils.DefaultOsProxy{}, paths.FromHome(home)))

	cmd, flagValue := newCachePushCmd(t, []string{})

	got, err := ResolveAndPersistCachePush(cmd, flagValue, nil)
	require.NoError(t, err)
	assert.False(t, got, "no flag change must honour stored value")
}

func TestResolveAndPersistCachePush_FlagUnchangedNoStoredFallsBackToDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd, flagValue := newCachePushCmd(t, []string{})

	got, err := ResolveAndPersistCachePush(cmd, flagValue, nil)
	require.NoError(t, err)
	assert.Equal(t, machineconfig.DefaultCachePush, got)
}

func TestResolveAndPersistCachePush_FlagRedundantDoesNotChurnFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	trueVal := true
	require.NoError(t, machineconfig.Write(machineconfig.Config{CachePush: &trueVal}, utils.DefaultOsProxy{}, paths.FromHome(home)))
	before, err := os.Stat(paths.FromHome(home).MachineConfigFile())
	require.NoError(t, err)

	cmd, flagValue := newCachePushCmd(t, []string{"--cache-push=true"})

	_, err = ResolveAndPersistCachePush(cmd, flagValue, nil)
	require.NoError(t, err)

	after, err := os.Stat(paths.FromHome(home).MachineConfigFile())
	require.NoError(t, err)
	assert.Equal(t, before.ModTime(), after.ModTime(), "redundant flag write must not touch the file")
}

func TestResolveAndPersistCachePush_PreservesProjectMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, machineconfig.Write(machineconfig.Config{ProjectMode: machineconfig.ModeOptIn}, utils.DefaultOsProxy{}, paths.FromHome(home)))

	cmd, flagValue := newCachePushCmd(t, []string{"--cache-push=false"})

	_, err := ResolveAndPersistCachePush(cmd, flagValue, nil)
	require.NoError(t, err)

	cfg, err := machineconfig.Read(utils.DefaultOsProxy{}, paths.FromHome(home), nil)
	require.NoError(t, err)
	assert.Equal(t, machineconfig.ModeOptIn, cfg.ProjectMode, "cache-push persistence must not clobber project mode")
	require.NotNil(t, cfg.CachePush)
	assert.False(t, *cfg.CachePush)
}
