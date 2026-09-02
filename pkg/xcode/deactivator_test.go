//go:build unit

package xcode_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xcodepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode"
)

func TestDeactivator_DryRun_LeavesShellRCUntouched(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	bashrc := filepath.Join(tmpHome, ".bashrc")
	seed := "# [start] Bitrise Xcelerate\nexport PATH=/x:$PATH\n# [end] Bitrise Xcelerate\n"
	require.NoError(t, os.WriteFile(bashrc, []byte(seed), 0o644))

	d := xcodepkg.NewDeactivator(xcodepkg.DeactivatorParams{
		DryRun: true,
		Logger: log.NewLogger(),
	})
	require.NoError(t, d.Deactivate(context.Background()))

	got, err := os.ReadFile(bashrc) //nolint:gosec // seeded tmp path
	require.NoError(t, err)
	assert.Equal(t, seed, string(got))
}
