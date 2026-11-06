//go:build unit

package enrichment

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneSessionSidecars_DropsStaleJSONAndEveryTmp(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	fresh := filepath.Join(dir, "fresh.json")
	stale := filepath.Join(dir, "stale.json")
	leftoverTmp := filepath.Join(dir, "leftover.json.tmp")

	require.NoError(t, os.WriteFile(fresh, []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(stale, []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(leftoverTmp, []byte("{}"), 0o600))

	oldTime := now.Add(-10 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(stale, oldTime, oldTime))

	pruned, err := pruneSessionSidecars(dir, now, SessionSidecarMaxAge)
	require.NoError(t, err)
	assert.Equal(t, 2, pruned, "stale.json + leftover.json.tmp pruned, fresh.json kept")

	remaining, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "fresh.json", remaining[0].Name())
}

func TestPruneSessionSidecars_MissingDirIsNoOp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	pruned, err := pruneSessionSidecars(dir, time.Now(), SessionSidecarMaxAge)
	require.NoError(t, err)
	assert.Zero(t, pruned)
}
