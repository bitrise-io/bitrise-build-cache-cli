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

// writeFixtureLog drops the shared SLF v13 fixture (65 Swift hits / 0 misses
// == 65/65, 100%) under derivedData/project/Logs/Build/name with the
// requested mtime. Replaces the old hand-crafted gzipped-text fixture that
// targeted the retired regex reader.
func writeFixtureLog(t *testing.T, derivedData, project, name string, mtime time.Time) string {
	t.Helper()

	logDir := filepath.Join(derivedData, project, "Logs", "Build")
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	body, err := os.ReadFile(filepath.Join("..", "xcelerate", "xcprobe", "slf", "testdata", "sample.xcactivitylog"))
	require.NoError(t, err)

	path := filepath.Join(logDir, name)
	require.NoError(t, os.WriteFile(path, body, 0o644))
	require.NoError(t, os.Chtimes(path, mtime, mtime))

	return path
}

// writeGarbageLog writes a non-SLF gzipped body so the decoder returns
// OutcomeUnparsed. Used only by the drift test.
func writeGarbageLog(t *testing.T, derivedData, project, name string, mtime time.Time) {
	t.Helper()

	logDir := filepath.Join(derivedData, project, "Logs", "Build")
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write([]byte("random content with no SLF header\n"))
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	path := filepath.Join(logDir, name)
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

func TestXcodeRecentBuildCheck_derivedDataMissing(t *testing.T) {
	res := diagnoseXcodeRecentBuild([]string{filepath.Join(t.TempDir(), "DerivedData")}, time.Now())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "no recent Xcode build found")
}

func TestXcodeRecentBuildCheck_noRecentBuild(t *testing.T) {
	dd := t.TempDir()
	now := time.Now()
	writeFixtureLog(t, dd, "MyApp-abc123", "AAA.xcactivitylog", now.Add(-2*time.Hour))

	res := diagnoseXcodeRecentBuild([]string{dd}, now)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "no recent Xcode build found")
}

func TestXcodeRecentBuildCheck_happyPath(t *testing.T) {
	dd := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	writeFixtureLog(t, dd, "MyApp-abc123def", "AAA-BBB.xcactivitylog", now.Add(-5*time.Minute))

	res := diagnoseXcodeRecentBuild([]string{dd}, now)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "recent xcode build: 65/65 hits (100%)")
	assert.Contains(t, res.Detail, "project MyApp-abc123def")
}

func TestXcodeRecentBuildCheck_picksNewestAcrossProjects(t *testing.T) {
	dd := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	writeFixtureLog(t, dd, "AppA-aaa", "old.xcactivitylog", now.Add(-30*time.Minute))
	// Newer entry must win — both fixtures carry the same 65/65, but the
	// Detail string names the project so the newest project should appear.
	writeFixtureLog(t, dd, "AppB-bbb", "new.xcactivitylog", now.Add(-1*time.Minute))

	res := diagnoseXcodeRecentBuild([]string{dd}, now)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "65/65 hits (100%)")
	assert.Contains(t, res.Detail, "project AppB-bbb")
}

func TestXcodeRecentBuildCheck_unparsedBuildReportsOK(t *testing.T) {
	// Unparsed must stay informational, not a doctor failure.
	dd := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	writeGarbageLog(t, dd, "MyApp-xyz", "junk.xcactivitylog", now.Add(-1*time.Minute))

	res := diagnoseXcodeRecentBuild([]string{dd}, now)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "regex drift")
}

func TestXcodeRecentBuildCheck_picksNewestAcrossDefaultAndCacheRoots(t *testing.T) {
	// Both the stock ~/Library/Developer/Xcode/DerivedData and the wrapper's
	// ~/.bitrise/cache/xcode-dd carry a recent log; the newer one must win.
	defaultDD := t.TempDir()
	cacheDD := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	writeFixtureLog(t, defaultDD, "DefaultProj-abc", "default.xcactivitylog", now.Add(-30*time.Minute))
	writeFixtureLog(t, cacheDD, "CacheProj-def", "cache.xcactivitylog", now.Add(-1*time.Minute))

	res := diagnoseXcodeRecentBuild([]string{defaultDD, cacheDD}, now)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "65/65 hits (100%)")
	assert.Contains(t, res.Detail, "project CacheProj-def")
	assert.Contains(t, res.Detail, "path "+filepath.Join(cacheDD, "CacheProj-def", "Logs", "Build", "cache.xcactivitylog"))
}

func TestXcodeRecentBuildCheck_defaultRootPickedWhenCacheEmpty(t *testing.T) {
	// Only the default DerivedData has a log — proves the default location is
	// scanned.
	defaultDD := t.TempDir()
	cacheDD := t.TempDir() // present but empty
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	writeFixtureLog(t, defaultDD, "DefaultProj-xyz", "default.xcactivitylog", now.Add(-5*time.Minute))

	res := diagnoseXcodeRecentBuild([]string{defaultDD, cacheDD}, now)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "65/65 hits")
	assert.Contains(t, res.Detail, "project DefaultProj-xyz")
}

func TestXcodeRecentBuildCheck_skippedWhenNotActivated(t *testing.T) {
	d := &Doctor{ActivatedTools: func() map[toolconfig.Tool]bool { return nil }}

	res := d.xcodeRecentBuildCheck().Diagnose(nil)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "skipped")
}
