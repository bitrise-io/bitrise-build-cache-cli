//go:build unit

package xcelerate_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
)

func TestDeactivate_Xcode_RemovesRootAndShellBlocks(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Simulate an activated state: create ~/.bitrise-xcelerate with a config file
	// and drop the "Bitrise Xcelerate" export block into both rc files.
	root := paths.FromHome(tmpHome).XcelerateRoot()
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "config.json"), []byte(`{}`), 0o644))

	bashRC := filepath.Join(tmpHome, ".bashrc")
	zshRC := filepath.Join(tmpHome, ".zshrc")
	bashBefore := "user=alice\n# [start] Bitrise Xcelerate\nexport PATH=/x:$PATH\n# [end] Bitrise Xcelerate\ntail=bash\n"
	zshBefore := "user=zed\n# [start] Bitrise Xcelerate\nexport PATH=/x:$PATH\n# [end] Bitrise Xcelerate\ntail=zsh\n"
	require.NoError(t, os.WriteFile(bashRC, []byte(bashBefore), 0o644))
	require.NoError(t, os.WriteFile(zshRC, []byte(zshBefore), 0o644))

	require.NoError(t, xcelerate.Deactivate(t.Context(), mockLogger, xcelerate.DeactivateParams{
		Envs: map[string]string{"HOME": tmpHome},
	}))

	_, err := os.Stat(root)
	assert.True(t, os.IsNotExist(err))

	bashAfter, err := os.ReadFile(bashRC)
	require.NoError(t, err)
	assert.Equal(t, "user=alice\ntail=bash\n", string(bashAfter))

	zshAfter, err := os.ReadFile(zshRC)
	require.NoError(t, err)
	assert.Equal(t, "user=zed\ntail=zsh\n", string(zshAfter))
}

func TestDeactivate_Xcode_Idempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	require.NoError(t, xcelerate.Deactivate(t.Context(), mockLogger, xcelerate.DeactivateParams{Envs: map[string]string{"HOME": tmpHome}}))
	require.NoError(t, xcelerate.Deactivate(t.Context(), mockLogger, xcelerate.DeactivateParams{Envs: map[string]string{"HOME": tmpHome}}))
}

func TestDeactivate_Xcode_DryRunPreservesArtefacts(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	root := paths.FromHome(tmpHome).XcelerateRoot()
	require.NoError(t, os.MkdirAll(root, 0o755))
	body := []byte(`{}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "config.json"), body, 0o644))

	bashRC := filepath.Join(tmpHome, ".bashrc")
	before := "# [start] Bitrise Xcelerate\nexport PATH=/x:$PATH\n# [end] Bitrise Xcelerate\n"
	require.NoError(t, os.WriteFile(bashRC, []byte(before), 0o644))

	require.NoError(t, xcelerate.Deactivate(t.Context(), mockLogger, xcelerate.DeactivateParams{
		Envs:   map[string]string{"HOME": tmpHome},
		DryRun: true,
	}))

	// Nothing removed / edited.
	got, err := os.ReadFile(filepath.Join(root, "config.json"))
	require.NoError(t, err)
	assert.Equal(t, body, got)

	bashAfter, err := os.ReadFile(bashRC)
	require.NoError(t, err)
	assert.Equal(t, before, string(bashAfter))
}

// Deactivate stops the proxy by pid. A launch agent left by an older CLI would
// restart it moments later, so deactivating has to retire that first or it does
// not deactivate anything.
func TestDeactivate_RetiresALeftoverLaunchAgent(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd-only: the linux arm shells out to systemctl")
	}

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	p := paths.FromHome(tmpHome)
	require.NoError(t, os.MkdirAll(p.LaunchAgentsDir(), 0o755))
	plist := p.PlistPath("io.bitrise.build-cache." + spawn.NameXcelerateProxy)
	require.NoError(t, os.WriteFile(plist, []byte("<plist/>"), 0o644))

	require.NoError(t, xcelerate.Deactivate(t.Context(), mockLogger, xcelerate.DeactivateParams{
		Envs: map[string]string{"HOME": tmpHome},
	}))

	assert.NoFileExists(t, plist)
}
