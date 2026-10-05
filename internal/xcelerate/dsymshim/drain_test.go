//go:build unit

package dsymshim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func writeTouch(t *testing.T, p paths.Paths, name string, rec touchRecord) {
	t.Helper()
	require.NoError(t, os.MkdirAll(p.XcelerateDsymShimDir(), 0o755))
	data, err := json.Marshal(rec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(p.XcelerateDsymShimDir(), name), append(data, '\n'), 0o644))
}

func TestDrain_AggregatesAndRemovesFiles(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)

	writeTouch(t, p, "sess-1.ndjson", touchRecord{Mode: "shim", FilteredStderr: 4, DurationMillis: 120})
	writeTouch(t, p, "sess-2.ndjson", touchRecord{Mode: "shim", MissedCount: 2, FilteredStderr: 4, DurationMillis: 80})
	writeTouch(t, p, "sess-3.ndjson", touchRecord{Mode: "bypass:killswitch", DurationMillis: 10})

	summary := Drain(p, "sess")
	assert.Equal(t, 2, summary.InvocationCount)
	assert.Equal(t, 1, summary.BypassCount)
	assert.Equal(t, 2, summary.Missed)
	assert.Equal(t, 8, summary.FilteredStderrLines)
	assert.Equal(t, int64(210), summary.TotalDurationMs)

	entries, err := os.ReadDir(p.XcelerateDsymShimDir())
	require.NoError(t, err)
	assert.Empty(t, entries, "touch files must be removed after drain")
}

func TestDrain_IgnoresFilesFromOtherSessions(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)

	writeTouch(t, p, "other-1.ndjson", touchRecord{Mode: "shim", MissedCount: 99})
	writeTouch(t, p, "mine-1.ndjson", touchRecord{Mode: "shim", MissedCount: 1})

	summary := Drain(p, "mine")
	assert.Equal(t, 1, summary.InvocationCount)
	assert.Equal(t, 1, summary.Missed)

	// Other-session file must survive — concurrent xcodebuild still needs it.
	_, err := os.Stat(filepath.Join(p.XcelerateDsymShimDir(), "other-1.ndjson"))
	assert.NoError(t, err)
}

func TestShimInstalled_ReturnsTrueOnlyWhenShimFilePresent(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	assert.False(t, ShimInstalled(p))

	require.NoError(t, os.MkdirAll(p.DsymutilCasShimToolchainBinDir(), 0o755))
	require.NoError(t, os.WriteFile(p.DsymutilCasShimPath(), []byte("x"), 0o755))
	assert.True(t, ShimInstalled(p))
}
