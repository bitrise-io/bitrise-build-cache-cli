//go:build unit

package analyzersidecar_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analyzersidecar"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

func fixedClock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func TestWriter_Write_RoundTripThroughJSON(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	w := &analyzersidecar.Writer{Paths: p, Clock: fixedClock(at)}

	rep := buildanalyzer.Report{
		Build: buildanalyzer.BuildMeta{Title: "Build Foo"},
		Tasks: []buildanalyzer.Task{{Key: "Ld /Foo", Target: "Foo", Tool: "linker"}},
	}

	require.NoError(t, w.Write(context.Background(), "inv-1", rep))

	final := p.AnalyzerSidecarFile("2026-11-25", "inv-1")
	body, err := os.ReadFile(final) //nolint:gosec // test file
	require.NoError(t, err)

	var got buildanalyzer.Report
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "Build Foo", got.Build.Title)
	require.Len(t, got.Tasks, 1)
	assert.Equal(t, "Ld /Foo", got.Tasks[0].Key)
}

func TestWriter_Write_FilePermsAre644(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	w := &analyzersidecar.Writer{Paths: p, Clock: fixedClock(time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC))}

	require.NoError(t, w.Write(context.Background(), "inv-perm", buildanalyzer.Report{}))

	info, err := os.Stat(p.AnalyzerSidecarFile("2026-11-25", "inv-perm"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestWriter_Write_TwoConcurrentWritesSameDirBothLand(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	w := &analyzersidecar.Writer{Paths: p, Clock: fixedClock(at)}

	var wg sync.WaitGroup
	ids := []string{"inv-a", "inv-b"}
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = w.Write(context.Background(), id, buildanalyzer.Report{Build: buildanalyzer.BuildMeta{Title: id}})
		}(id)
	}
	wg.Wait()

	for _, id := range ids {
		info, err := os.Stat(p.AnalyzerSidecarFile("2026-11-25", id))
		require.NoError(t, err, "sidecar for %s must exist", id)
		assert.Greater(t, info.Size(), int64(0))
	}

	entries, err := os.ReadDir(p.AnalyzerSidecarDir("2026-11-25"))
	require.NoError(t, err)
	assert.Len(t, entries, 2, "no stray tmp files after concurrent writes")
}

func TestWriter_Write_AtomicUnderTmpDir(t *testing.T) {
	// After a successful write, no `.tmp-*` siblings may linger in the per-day
	// dir — the rename must be the only artifact the caller sees.
	home := t.TempDir()
	p := paths.FromHome(home)
	w := &analyzersidecar.Writer{Paths: p, Clock: fixedClock(time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC))}

	require.NoError(t, w.Write(context.Background(), "inv-atomic", buildanalyzer.Report{}))

	entries, err := os.ReadDir(p.AnalyzerSidecarDir("2026-11-25"))
	require.NoError(t, err)

	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".tmp-"), "found lingering tmp file: %s", e.Name())
	}
}

func TestWriter_Write_CtxCancelSkipsWrite(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	w := &analyzersidecar.Writer{Paths: p, Clock: fixedClock(time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC))}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := w.Write(ctx, "inv-cancel", buildanalyzer.Report{})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// No file should exist.
	_, err = os.Stat(p.AnalyzerSidecarFile("2026-11-25", "inv-cancel"))
	assert.True(t, os.IsNotExist(err))
}

func TestWriter_Write_EmptyInvocationIDErrors(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	w := &analyzersidecar.Writer{Paths: p, Clock: fixedClock(time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC))}

	require.Error(t, w.Write(context.Background(), "", buildanalyzer.Report{}))
}

func TestWriter_Write_OverwritesExisting(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	w := &analyzersidecar.Writer{Paths: p, Clock: fixedClock(time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC))}

	require.NoError(t, w.Write(context.Background(), "inv-twice", buildanalyzer.Report{Build: buildanalyzer.BuildMeta{Title: "first"}}))
	require.NoError(t, w.Write(context.Background(), "inv-twice", buildanalyzer.Report{Build: buildanalyzer.BuildMeta{Title: "second"}}))

	final := p.AnalyzerSidecarFile("2026-11-25", "inv-twice")
	body, err := os.ReadFile(final) //nolint:gosec // test file
	require.NoError(t, err)

	var got buildanalyzer.Report
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "second", got.Build.Title)
}

func TestWriter_Write_EnsuresPerDayDirExists(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	w := &analyzersidecar.Writer{Paths: p, Clock: fixedClock(time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC))}

	require.NoError(t, w.Write(context.Background(), "inv-mkdir", buildanalyzer.Report{}))

	info, err := os.Stat(filepath.Dir(p.AnalyzerSidecarFile("2026-11-25", "inv-mkdir")))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
