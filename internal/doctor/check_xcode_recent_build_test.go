//go:build unit

package doctor

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

// writeGzipLog creates <project>/Logs/Build/<name>.xcactivitylog under
// derivedData, wrapping body in gzip and setting mtime.
func writeGzipLog(t *testing.T, derivedData, project, name string, body []byte, mtime time.Time) string {
	t.Helper()

	logDir := filepath.Join(derivedData, project, "Logs", "Build")
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(body)
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	path := filepath.Join(logDir, name)
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	require.NoError(t, os.Chtimes(path, mtime, mtime))

	return path
}

func TestXcodeRecentBuildCheck_derivedDataMissing(t *testing.T) {
	// Fresh temp with no DerivedData subtree — reports OK (informational), not a failure.
	res := diagnoseXcodeRecentBuild(filepath.Join(t.TempDir(), "DerivedData"), time.Now())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "no recent Xcode build found")
}

func TestXcodeRecentBuildCheck_noRecentBuild(t *testing.T) {
	// Log present but older than the window → no match.
	dd := t.TempDir()
	now := time.Now()
	writeGzipLog(t, dd, "MyApp-abc123", "AAA.xcactivitylog",
		[]byte("note: 10 hits / 10 cacheable tasks (100%)\n"),
		now.Add(-2*time.Hour))

	res := diagnoseXcodeRecentBuild(dd, now)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "no recent Xcode build found")
}

func TestXcodeRecentBuildCheck_happyPath(t *testing.T) {
	dd := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	writeGzipLog(t, dd, "MyApp-abc123def", "AAA-BBB.xcactivitylog",
		[]byte("header\nnote: 7 hits / 10 cacheable tasks (70%)\n"),
		now.Add(-5*time.Minute))

	res := diagnoseXcodeRecentBuild(dd, now)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "recent xcode build: 7/10 hits (70%)")
	assert.Contains(t, res.Detail, "project MyApp-abc123def")
}

func TestXcodeRecentBuildCheck_picksNewestAcrossProjects(t *testing.T) {
	dd := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	// Older match in project A.
	writeGzipLog(t, dd, "AppA-aaa", "old.xcactivitylog",
		[]byte("note: 1 hits / 2 cacheable tasks (50%)\n"),
		now.Add(-30*time.Minute))
	// Newer match in project B — must win.
	writeGzipLog(t, dd, "AppB-bbb", "new.xcactivitylog",
		[]byte("note: 9 hits / 10 cacheable tasks (90%)\n"),
		now.Add(-1*time.Minute))

	res := diagnoseXcodeRecentBuild(dd, now)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "9/10 hits (90%)")
	assert.Contains(t, res.Detail, "project AppB-bbb")
}

func TestXcodeRecentBuildCheck_unparsedBuildReportsOK(t *testing.T) {
	// Random SLF noise with no CompilationCacheMetrics line — still informational,
	// never a doctor failure.
	dd := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	writeGzipLog(t, dd, "MyApp-xyz", "junk.xcactivitylog",
		[]byte("random SLF content with no match\n"),
		now.Add(-1*time.Minute))

	res := diagnoseXcodeRecentBuild(dd, now)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "regex drift")
}

func TestXcodeRecentBuildCheck_skippedWhenNotActivated(t *testing.T) {
	d := &Doctor{ActivatedTools: func() map[toolconfig.Tool]bool { return nil }}

	res := d.xcodeRecentBuildCheck().Diagnose(nil)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "skipped")
}
