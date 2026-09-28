//go:build unit

package bazel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func TestInstallCredHelperShim_writesExecutableShim(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tools")

	installShimDir = target
	installShimVersion = "v9.9.9"
	t.Cleanup(func() { installShimDir = ""; installShimVersion = "" })

	require.NoError(t, installCredHelperShimCmd.RunE(installCredHelperShimCmd, nil))

	shim := filepath.Join(target, paths.BazelCredHelperShimName)
	info, err := os.Stat(shim)
	require.NoError(t, err)
	assert.True(t, info.Mode()&0o111 != 0, "shim must be executable")

	body, err := os.ReadFile(shim)
	require.NoError(t, err)

	assert.Contains(t, string(body), "v9.9.9")
	assert.Contains(t, string(body), "installer.sh")
	assert.Contains(t, string(body), "command -v bitrise-build-cache")
}

func TestInstallCredHelperShim_idempotent(t *testing.T) {
	dir := t.TempDir()
	installShimDir = dir
	installShimVersion = "v1.0.0"
	t.Cleanup(func() { installShimDir = ""; installShimVersion = "" })

	require.NoError(t, installCredHelperShimCmd.RunE(installCredHelperShimCmd, nil))
	require.NoError(t, installCredHelperShimCmd.RunE(installCredHelperShimCmd, nil))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var shims int
	for _, e := range entries {
		if strings.Contains(e.Name(), "credhelper") {
			shims++
		}
	}
	assert.Equal(t, 1, shims)
}
