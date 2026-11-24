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

// TestPickSnapshotForLog_PicksEarliestAfterCutoff seeds three snapshots
// straddling the cutoff and verifies the picker returns the earliest post-log
// snapshot as current (the one captured soonest after the build finished)
// with the newest pre-log snapshot as prior.
func TestPickSnapshotForLog_PicksEarliestAfterCutoff(t *testing.T) {
	storeRoot := t.TempDir()
	hex := "d3b07384d113edec49eaa6238ad5ff00"
	dir := filepath.Join(storeRoot, hex)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// Snapshot timestamps: 100, 200, 300 unix seconds.
	for i, ts := range []int64{100, 200, 300} {
		fname := fmt.Sprintf("%010d-%d.db", i+1, ts)
		require.NoError(t, os.WriteFile(filepath.Join(dir, fname), []byte("x"), 0o600))
	}

	// Cutoff at 150 → post-log=[200,300] (earliest = 200 = iter 2);
	// pre-log=[100] → prior iter 1.
	current, prior, ok := PickSnapshotForLog(storeRoot, hex, time.Unix(150, 0))
	require.True(t, ok)
	assert.Equal(t, int64(2), current.Iteration)
	assert.Equal(t, int64(200), current.Timestamp.Unix())
	assert.Equal(t, int64(1), prior.Iteration)
	assert.Equal(t, int64(100), prior.Timestamp.Unix())
}

// TestPickSnapshotForLog_NoneAfterCutoff_FallsBackToPreLog covers the "user
// forgot to snapshot after the build" case: every snapshot pre-dates the log.
// Picker falls back to newest-pre-log as current, next-older as prior.
func TestPickSnapshotForLog_NoneAfterCutoff_FallsBackToPreLog(t *testing.T) {
	storeRoot := t.TempDir()
	hex := "abc"
	dir := filepath.Join(storeRoot, hex)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	for i, ts := range []int64{50, 100} {
		fname := fmt.Sprintf("%010d-%d.db", i+1, ts)
		require.NoError(t, os.WriteFile(filepath.Join(dir, fname), []byte("x"), 0o600))
	}

	current, prior, ok := PickSnapshotForLog(storeRoot, hex, time.Unix(500, 0))
	require.True(t, ok, "pre-log fallback should succeed")
	assert.Equal(t, int64(2), current.Iteration)
	assert.Equal(t, int64(1), prior.Iteration)
}

// TestPickSnapshotForLog_SinglePostLog_NoPrior verifies that when the only
// snapshot is post-log and nothing precedes the cutoff, prior stays empty
// but ok=true.
func TestPickSnapshotForLog_SinglePostLog_NoPrior(t *testing.T) {
	storeRoot := t.TempDir()
	hex := "abc"
	dir := filepath.Join(storeRoot, hex)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "0000000001-500.db"), []byte("x"), 0o600))

	current, prior, ok := PickSnapshotForLog(storeRoot, hex, time.Unix(100, 0))
	require.True(t, ok)
	assert.Equal(t, int64(1), current.Iteration)
	assert.Empty(t, prior.Path)
}

// TestPickSnapshotForLog_EmptyStoreReturnsFalse covers the fresh-root case.
func TestPickSnapshotForLog_EmptyStoreReturnsFalse(t *testing.T) {
	_, _, ok := PickSnapshotForLog(t.TempDir(), "any-hex", time.Now())
	assert.False(t, ok)
}
