//go:build unit

package enrichment_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func newTestLogger() *utilsMocks.Logger {
	l := &utilsMocks.Logger{}
	for _, name := range []string{"Debugf", "Infof", "Warnf", "Errorf", "Printf",
		"TDebugf", "TInfof", "TWarnf", "TErrorf", "TDonef", "TPrintf", "Donef", "Println"} {
		l.On(name, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	}
	l.On("EnableDebugLog", mock.Anything).Return()

	return l
}

func TestWriteMarker_createsFileUnderStateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	enrichment.WriteMarker(newTestLogger(), "inv-abc")

	marker := paths.FromHome(home).XcelerateHandledInvocationFile("inv-abc")
	info, err := os.Stat(marker)
	require.NoError(t, err, "marker file must exist after successful write")
	assert.False(t, info.IsDir())
}

func TestWriteMarker_emptyIDIsNoop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	enrichment.WriteMarker(newTestLogger(), "")

	entries, err := os.ReadDir(paths.FromHome(home).XcelerateHandledInvocationDir())
	if err != nil {
		return
	}
	assert.Empty(t, entries, "empty invocation ID must not write any marker file")
}

func TestMarkerExists_trueWhenPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.XcelerateHandledInvocationDir(), 0o755))
	require.NoError(t, os.WriteFile(p.XcelerateHandledInvocationFile("inv-1"), nil, 0o644))

	assert.True(t, enrichment.MarkerExists("inv-1"))
	assert.False(t, enrichment.MarkerExists("inv-2"))
	assert.False(t, enrichment.MarkerExists(""))
}

func TestPruneStale_removesStaleKeepsFresh(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "stale")
	fresh := filepath.Join(dir, "fresh")
	require.NoError(t, os.WriteFile(stale, nil, 0o644))
	require.NoError(t, os.WriteFile(fresh, nil, 0o644))

	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))

	enrichment.PruneStale(dir, 24*time.Hour)

	_, err := os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "stale marker must be removed")
	_, err = os.Stat(fresh)
	assert.NoError(t, err, "fresh marker must survive")
}

func TestPruneStale_missingDirIsNoop(t *testing.T) {
	assert.NotPanics(t, func() {
		enrichment.PruneStale(filepath.Join(t.TempDir(), "does-not-exist"), time.Hour)
	})
}
