//go:build unit

package childstats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	return home
}

func TestLedgerPaths(t *testing.T) {
	setHome(t)

	dir := LedgerDir("parent-x")
	assert.True(t, filepath.IsAbs(dir))
	assert.Contains(t, dir, filepath.Join(".bitrise", "cache", "invocations", "parent-x"))

	path := LedgerPath("parent-x", "child-y")
	assert.Equal(t, filepath.Join(dir, "child-y.childstats.json"), path)
}

func TestWriter_Write_CreatesAtomicFile(t *testing.T) {
	setHome(t)

	entry := Entry{
		ChildInvocationID:  "child-1",
		ParentInvocationID: "parent-1",
		BuildTool:          "gradle",
		HitRate:            0.75,
		Hits:               3,
		Total:              4,
	}

	require.NoError(t, NewWriter().Write(entry))

	path := LedgerPath("parent-1", "child-1")
	data, err := os.ReadFile(path) //nolint:gosec
	require.NoError(t, err)

	var got Entry
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "child-1", got.ChildInvocationID)
	assert.Equal(t, "parent-1", got.ParentInvocationID)
	assert.Equal(t, "gradle", got.BuildTool)
	assert.InDelta(t, 0.75, got.HitRate, 1e-6)
	assert.Equal(t, int64(3), got.Hits)
	assert.Equal(t, int64(4), got.Total)
	assert.Equal(t, SchemaVersion, got.SchemaVersion)
	assert.False(t, got.WrittenAt.IsZero())

	// No stray temp files next to the final entry.
	dirEntries, err := os.ReadDir(LedgerDir("parent-1"))
	require.NoError(t, err)
	for _, de := range dirEntries {
		assert.False(t, filepath.Ext(de.Name()) == ".tmp", "unexpected tmp leftover: %s", de.Name())
	}
}

func TestWriter_Write_NoOpWhenIDsMissing(t *testing.T) {
	home := setHome(t)

	require.NoError(t, NewWriter().Write(Entry{ChildInvocationID: "c"}))
	require.NoError(t, NewWriter().Write(Entry{ParentInvocationID: "p"}))

	// No directory should have been created.
	_, err := os.Stat(filepath.Join(home, ".bitrise", "cache", "invocations"))
	assert.True(t, os.IsNotExist(err))
}

func TestAggregator_Compute_MissingDirReturnsEmpty(t *testing.T) {
	setHome(t)

	summary, err := NewAggregator("nobody").Compute()
	require.NoError(t, err)
	assert.Equal(t, 0, summary.ChildCount)
	assert.Equal(t, float32(0), summary.MeanHitRate)
}

func TestAggregator_Compute_SimpleMeanAndByTool(t *testing.T) {
	setHome(t)

	w := NewWriter()
	require.NoError(t, w.Write(Entry{ParentInvocationID: "p", ChildInvocationID: "a", BuildTool: "gradle", HitRate: 0.2}))
	require.NoError(t, w.Write(Entry{ParentInvocationID: "p", ChildInvocationID: "b", BuildTool: "gradle", HitRate: 0.8}))
	require.NoError(t, w.Write(Entry{ParentInvocationID: "p", ChildInvocationID: "c", BuildTool: "ccache", HitRate: 0.5}))

	summary, err := NewAggregator("p").Compute()
	require.NoError(t, err)

	assert.Equal(t, 3, summary.ChildCount)
	assert.Equal(t, 0, summary.SkippedCount)
	assert.InDelta(t, 0.5, summary.MeanHitRate, 1e-6)

	gradle, ok := summary.ByTool["gradle"]
	require.True(t, ok)
	assert.Equal(t, 2, gradle.Count)
	assert.InDelta(t, 0.5, gradle.MeanHitRate, 1e-6)

	ccache, ok := summary.ByTool["ccache"]
	require.True(t, ok)
	assert.Equal(t, 1, ccache.Count)
	assert.InDelta(t, 0.5, ccache.MeanHitRate, 1e-6)
}

func TestAggregator_Compute_IncludesBaselineWithCount(t *testing.T) {
	setHome(t)

	w := NewWriter()
	require.NoError(t, w.Write(Entry{ParentInvocationID: "p", ChildInvocationID: "a", BuildTool: "gradle", HitRate: 0.9}))
	require.NoError(t, w.Write(Entry{
		ParentInvocationID: "p",
		ChildInvocationID:  "b",
		BuildTool:          "gradle",
		HitRate:            0.0,
		BenchmarkPhase:     BenchmarkPhaseBaseline,
	}))

	summary, err := NewAggregator("p").Compute()
	require.NoError(t, err)
	assert.Equal(t, 2, summary.ChildCount)
	assert.Equal(t, 1, summary.BaselineCount)
	assert.Equal(t, 0, summary.SkippedCount)
	assert.InDelta(t, 0.45, summary.MeanHitRate, 1e-6)
}

func TestAggregator_Compute_WeightedHitRate(t *testing.T) {
	setHome(t)

	w := NewWriter()
	require.NoError(t, w.Write(Entry{
		ParentInvocationID: "p", ChildInvocationID: "small", BuildTool: "gradle",
		HitRate: 1.0, Hits: 1, Total: 1,
	}))
	require.NoError(t, w.Write(Entry{
		ParentInvocationID: "p", ChildInvocationID: "big", BuildTool: "gradle",
		HitRate: 0.5, Hits: 50, Total: 100,
	}))

	summary, err := NewAggregator("p").Compute()
	require.NoError(t, err)

	assert.InDelta(t, 0.75, summary.MeanHitRate, 1e-6)
	assert.InDelta(t, 51.0/101.0, summary.WeightedHitRate, 1e-6)
	assert.Equal(t, int64(51), summary.TotalHits)
	assert.Equal(t, int64(101), summary.TotalCount)

	gradle := summary.ByTool["gradle"]
	assert.InDelta(t, 51.0/101.0, gradle.WeightedHitRate, 1e-6)
	assert.Equal(t, int64(51), gradle.TotalHits)
	assert.Equal(t, int64(101), gradle.TotalCount)
}

func TestAggregator_Compute_SkipsMalformedFiles(t *testing.T) {
	setHome(t)

	w := NewWriter()
	require.NoError(t, w.Write(Entry{ParentInvocationID: "p", ChildInvocationID: "good", BuildTool: "gradle", HitRate: 0.4}))

	bad := filepath.Join(LedgerDir("p"), "broken"+EntryFileSuffix)
	require.NoError(t, os.WriteFile(bad, []byte("{not json"), 0o600))

	summary, err := NewAggregator("p").Compute()
	require.NoError(t, err)
	assert.Equal(t, 1, summary.ChildCount)
	assert.Equal(t, 1, summary.SkippedCount)
	assert.InDelta(t, 0.4, summary.MeanHitRate, 1e-6)
}

func TestAggregator_Compute_IgnoresUnrelatedFilesAndSubdirs(t *testing.T) {
	setHome(t)

	w := NewWriter()
	require.NoError(t, w.Write(Entry{ParentInvocationID: "p", ChildInvocationID: "a", BuildTool: "gradle", HitRate: 0.3}))

	dir := LedgerDir("p")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o600))
	// Plain .json without the .childstats. suffix is reserved for other ledgers — must be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.json"), []byte(`{"foo":"bar"}`), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))

	summary, err := NewAggregator("p").Compute()
	require.NoError(t, err)
	assert.Equal(t, 1, summary.ChildCount)
	assert.Equal(t, 0, summary.SkippedCount)
}

func TestSweep_RemovesStaleDirsKeepsFresh(t *testing.T) {
	setHome(t)

	w := NewWriter()
	require.NoError(t, w.Write(Entry{ParentInvocationID: "fresh", ChildInvocationID: "a", BuildTool: "gradle", HitRate: 0.5}))
	require.NoError(t, w.Write(Entry{ParentInvocationID: "stale", ChildInvocationID: "a", BuildTool: "gradle", HitRate: 0.5}))

	staleDir := LedgerDir("stale")
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(staleDir, old, old))

	require.NoError(t, Sweep(24*time.Hour))

	_, err := os.Stat(staleDir)
	assert.True(t, os.IsNotExist(err), "stale dir should be removed")

	_, err = os.Stat(LedgerDir("fresh"))
	assert.NoError(t, err, "fresh dir should be preserved")
}

func TestSweep_MissingRootIsNoOp(t *testing.T) {
	setHome(t)

	require.NoError(t, Sweep(24*time.Hour))
}

func TestSweep_IgnoresNonDirEntries(t *testing.T) {
	setHome(t)

	require.NoError(t, NewWriter().Write(Entry{ParentInvocationID: "p", ChildInvocationID: "a", BuildTool: "gradle", HitRate: 0.5}))

	root := filepath.Dir(LedgerDir("p"))
	require.NoError(t, os.WriteFile(filepath.Join(root, "stray.txt"), []byte("hi"), 0o600))

	require.NoError(t, Sweep(24*time.Hour))

	_, err := os.Stat(LedgerDir("p"))
	assert.NoError(t, err)
}

func TestAggregator_Cleanup_RemovesDir(t *testing.T) {
	setHome(t)

	require.NoError(t, NewWriter().Write(Entry{ParentInvocationID: "p", ChildInvocationID: "a", BuildTool: "gradle", HitRate: 0.5}))

	agg := NewAggregator("p")
	require.NoError(t, agg.Cleanup())

	_, err := os.Stat(LedgerDir("p"))
	assert.True(t, os.IsNotExist(err))

	// Cleanup on a missing dir is a no-op, not an error.
	require.NoError(t, agg.Cleanup())
}
