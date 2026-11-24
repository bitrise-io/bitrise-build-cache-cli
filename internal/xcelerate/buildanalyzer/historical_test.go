//go:build unit

package buildanalyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPickSnapshotForLog_PicksMostRecentBeforeOrAtCutoff seeds three snapshots
// with ascending timestamps and verifies that the picker returns the newest
// whose Timestamp <= cutoff, with the next-older as prior.
func TestPickSnapshotForLog_PicksMostRecentBeforeOrAtCutoff(t *testing.T) {
	storeRoot := t.TempDir()
	hex := "d3b07384d113edec49eaa6238ad5ff00"
	dir := filepath.Join(storeRoot, hex)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Snapshot timestamps: 100, 200, 300 unix seconds.
	// Iterations deliberately in the same order to make the tiebreak clear.
	for i, ts := range []int64{100, 200, 300} {
		fname := fmt.Sprintf("%010d-%d.db", i+1, ts)
		require.NoError(t, os.WriteFile(filepath.Join(dir, fname), []byte("x"), 0o600))
	}

	// Cutoff at 250 → should pick timestamp=200 (iter=2) with prior=100 (iter=1).
	current, prior, ok := PickSnapshotForLog(storeRoot, hex, time.Unix(250, 0))
	require.True(t, ok)
	assert.Equal(t, int64(2), current.Iteration)
	assert.Equal(t, int64(100), prior.Timestamp.Unix())
	assert.Equal(t, int64(1), prior.Iteration)
}

// TestPickSnapshotForLog_NoneBeforeCutoff covers the "store exists but every
// snapshot is newer than the log" case — picker returns ok=false.
func TestPickSnapshotForLog_NoneBeforeCutoff(t *testing.T) {
	storeRoot := t.TempDir()
	hex := "abc"
	dir := filepath.Join(storeRoot, hex)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "0000000001-500.db"), []byte("x"), 0o600))

	_, _, ok := PickSnapshotForLog(storeRoot, hex, time.Unix(100, 0))
	assert.False(t, ok, "snapshot ts=500 is after cutoff ts=100 → no match")
}

// TestPickSnapshotForLog_SingleSnapshotNoPrior verifies that when only one
// snapshot matches, the prior comes back empty but ok stays true.
func TestPickSnapshotForLog_SingleSnapshotNoPrior(t *testing.T) {
	storeRoot := t.TempDir()
	hex := "abc"
	dir := filepath.Join(storeRoot, hex)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "0000000001-100.db"), []byte("x"), 0o600))

	current, prior, ok := PickSnapshotForLog(storeRoot, hex, time.Unix(200, 0))
	require.True(t, ok)
	assert.Equal(t, int64(1), current.Iteration)
	assert.Empty(t, prior.Path)
}

// TestPickSnapshotForLog_EmptyStoreReturnsFalse covers the fresh-root case.
func TestPickSnapshotForLog_EmptyStoreReturnsFalse(t *testing.T) {
	_, _, ok := PickSnapshotForLog(t.TempDir(), "any-hex", time.Now())
	assert.False(t, ok)
}
