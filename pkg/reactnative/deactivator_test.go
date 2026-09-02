//go:build unit

package reactnative

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeactivator_DryRun_LeavesMarkerUntouched(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	marker := filepath.Join(tmpHome, ".bitrise", "cache", "reactnative", "config.json")
	seed := `{"enabled":true}`
	require.NoError(t, os.MkdirAll(filepath.Dir(marker), 0o755))
	require.NoError(t, os.WriteFile(marker, []byte(seed), 0o644))

	d := NewDeactivator(DeactivatorParams{
		DryRun: true,
		Logger: log.NewLogger(),
	})
	require.NoError(t, d.Deactivate(context.Background()))

	got, err := os.ReadFile(marker) //nolint:gosec // seeded tmp path
	require.NoError(t, err)
	assert.Equal(t, seed, string(got))
}
