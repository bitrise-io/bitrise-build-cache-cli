//go:build unit

package xcodeargs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/dsymshim"
)

func TestDsymutilShimToolchainsArg_PresentWhenStaged(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)

	require.NoError(t, os.MkdirAll(p.DsymutilCasShimToolchainBinDir(), 0o755))
	require.NoError(t, os.WriteFile(p.DsymutilCasShimPath(), []byte("#!/bin/sh\nexit 0\n"), 0o755))

	got := DsymutilShimToolchainsArg(p, utils.DefaultOsProxy{}, map[string]string{})
	assert.Equal(t, map[string]string{ToolchainsKey: DsymutilCasShimToolchainsValue}, got)
}

func TestDsymutilShimToolchainsArg_AbsentWhenKillswitchSet(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.DsymutilCasShimToolchainBinDir(), 0o755))
	require.NoError(t, os.WriteFile(p.DsymutilCasShimPath(), []byte("x"), 0o755))

	got := DsymutilShimToolchainsArg(p, utils.DefaultOsProxy{}, map[string]string{dsymshim.EnvKillSwitch: "1"})
	assert.Nil(t, got)
}

func TestDsymutilShimToolchainsArg_AbsentWhenShimMissing(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	// Dir exists but no shim file.
	require.NoError(t, os.MkdirAll(filepath.Dir(p.DsymutilCasShimPath()), 0o755))

	got := DsymutilShimToolchainsArg(p, utils.DefaultOsProxy{}, map[string]string{})
	assert.Nil(t, got)
}
