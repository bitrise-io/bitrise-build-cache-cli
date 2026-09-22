package jobsummary_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/jobsummary"
)

func snapshotWith(t *testing.T, downloads int, latency time.Duration) *blobstats.Snapshot {
	t.Helper()

	c := blobstats.NewCollector()
	for range downloads {
		c.Download.RecordTransfer(1024*1024, latency)
	}

	s := c.Snapshot()

	return &s
}

func TestRenderEmptyWithoutWorkOrTransfer(t *testing.T) {
	assert.Empty(t, jobsummary.Summary{Tool: "Xcode", Unit: "tasks"}.Render())
}

func TestRenderReportsHitsAndLatency(t *testing.T) {
	md := jobsummary.Summary{
		Tool:          "Xcode",
		Unit:          "tasks",
		Hits:          70,
		Total:         100,
		BlobStats:     snapshotWith(t, 4, 5*time.Millisecond),
		InvocationURL: "https://app.bitrise.io/build-cache/invocations/xcode/abc",
	}.Render()

	assert.Contains(t, md, "70.0% of Xcode work avoided")
	assert.Contains(t, md, "| From cache | 70 |")
	assert.Contains(t, md, "| Executed | 30 |")
	// 5ms lands in the <=8ms bucket, and upload has no ops at all.
	assert.Contains(t, md, "| Latency p50 | 8 ms | — |")
	assert.Contains(t, md, "invocations/xcode/abc")
}

// Transfer alone is worth a section: a fully cached build still moved blobs.
func TestRenderWithoutTaskCounts(t *testing.T) {
	md := jobsummary.Summary{Tool: "ccache", Unit: "compilations", BlobStats: snapshotWith(t, 2, 3*time.Millisecond)}.Render()

	assert.NotContains(t, md, "work avoided")
	assert.Contains(t, md, "### Cache transfer")
}

func TestWriteReplacesOwnSectionAndKeepsOthers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.md")
	require.NoError(t, os.WriteFile(path, []byte("<!-- bitrise-build-cache:start -->\ngradle\n<!-- bitrise-build-cache:end -->\n"), 0o600))
	t.Setenv("GITHUB_STEP_SUMMARY", path)

	for _, body := range []string{"first\n", "second\n"} {
		written, err := jobsummary.Write("xcode", body)
		require.NoError(t, err)
		assert.True(t, written)
	}

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.Equal(t, 1, strings.Count(string(content), "<!-- bitrise-build-cache:xcode:start -->"))
	assert.Contains(t, string(content), "second")
	assert.NotContains(t, string(content), "first")
	assert.Contains(t, string(content), "gradle", "the Gradle plugins' own section must survive")
}

func TestWriteIsANoOpOffGitHubActions(t *testing.T) {
	t.Setenv("GITHUB_STEP_SUMMARY", "")

	written, err := jobsummary.Write("xcode", "anything")
	require.NoError(t, err)
	assert.False(t, written)
}
