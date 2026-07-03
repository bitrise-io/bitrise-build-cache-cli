//go:build unit

package invocations

import (
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
)

const canonicalRecordPath = "../../docs/canonical-record.ndjson"

func newTestWriter(t *testing.T, at time.Time) (*Writer, paths.Paths) {
	t.Helper()

	p := paths.FromHome(t.TempDir())
	w := NewWriter(p)
	w.Clock = func() time.Time { return at }

	return w, p
}

func TestWriter_Append_writesDailyNDJSON(t *testing.T) {
	at := time.Date(2026, 6, 25, 13, 14, 15, 0, time.UTC)
	w, p := newTestWriter(t, at)

	rec := Record{
		InvocationID: "inv-1",
		Command:      "activate gradle",
		Tool:         ToolGradle,
		CLIVersion:   "v2.8.6",
		StartedAt:    at,
		ExitCode:     0,
		Source:       SourceLocal,
	}
	require.NoError(t, w.Append(rec))

	want := p.InvocationsFile("2026-06-25")
	b, err := os.ReadFile(want)
	require.NoError(t, err)

	var got Record
	require.NoError(t, json.Unmarshal([]byte(strings.TrimRight(string(b), "\n")), &got))
	assert.Equal(t, rec.InvocationID, got.InvocationID)
	assert.Equal(t, rec.Command, got.Command)
	assert.Equal(t, rec.Tool, got.Tool)
}

func TestWriter_Append_rotatesByDay(t *testing.T) {
	clock := time.Date(2026, 6, 24, 23, 59, 59, 0, time.UTC)
	w, p := newTestWriter(t, clock)

	require.NoError(t, w.Append(Record{InvocationID: "yesterday"}))

	// roll over to next UTC day
	w.Clock = func() time.Time { return clock.Add(2 * time.Second) }
	require.NoError(t, w.Append(Record{InvocationID: "today"}))

	yesterday, err := os.ReadFile(p.InvocationsFile("2026-06-24"))
	require.NoError(t, err)
	today, err := os.ReadFile(p.InvocationsFile("2026-06-25"))
	require.NoError(t, err)

	assert.Contains(t, string(yesterday), "yesterday")
	assert.Contains(t, string(today), "today")
	assert.NotContains(t, string(yesterday), "today")
	assert.NotContains(t, string(today), "yesterday")
}

func TestWriter_Append_writesOversizedRecordVerbatim(t *testing.T) {
	at := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	w, p := newTestWriter(t, at)

	cmd := strings.Repeat("x", 8000)
	rec := Record{
		InvocationID: "big",
		Command:      cmd,
		Tool:         ToolXcode,
		CLIVersion:   "v2.8.6",
		StartedAt:    at,
		Source:       SourceLocal,
	}
	require.NoError(t, w.Append(rec))

	body, err := os.ReadFile(p.InvocationsFile("2026-06-25"))
	require.NoError(t, err)

	var got Record
	require.NoError(t, json.Unmarshal([]byte(strings.TrimRight(string(body), "\n")), &got))
	assert.Equal(t, cmd, got.Command)
}

func TestWriter_Append_concurrentWritesParseable(t *testing.T) {
	at := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	w, p := newTestWriter(t, at)

	const writers = 16
	const perWriter = 50

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				_ = w.Append(Record{
					InvocationID: "inv",
					Command:      "ci",
					Tool:         ToolXcode,
					CLIVersion:   "v2.8.6",
					StartedAt:    at,
					Source:       SourceLocal,
				})
			}
		}(i)
	}
	wg.Wait()

	b, err := os.ReadFile(p.InvocationsFile("2026-06-25"))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	assert.Len(t, lines, writers*perWriter, "no records dropped")
	for _, l := range lines {
		var rec Record
		assert.NoError(t, json.Unmarshal([]byte(l), &rec), "line not parseable as JSON: %q", l)
	}
}

func TestReader_Recent_acrossDailyFiles(t *testing.T) {
	day1 := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	day3 := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	w, p := newTestWriter(t, day1)
	require.NoError(t, w.Append(Record{InvocationID: "a"}))
	require.NoError(t, w.Append(Record{InvocationID: "b"}))

	w.Clock = func() time.Time { return day2 }
	require.NoError(t, w.Append(Record{InvocationID: "c"}))

	w.Clock = func() time.Time { return day3 }
	require.NoError(t, w.Append(Record{InvocationID: "d"}))
	require.NoError(t, w.Append(Record{InvocationID: "e"}))

	r := NewReader(p)
	recs, err := r.Recent(3)
	require.NoError(t, err)

	ids := make([]string, len(recs))
	for i, rec := range recs {
		ids[i] = rec.InvocationID
	}
	assert.Equal(t, []string{"c", "d", "e"}, ids)
}

func TestReader_Recent_emptyDir(t *testing.T) {
	r := NewReader(paths.FromHome(t.TempDir()))

	recs, err := r.Recent(10)
	require.NoError(t, err)
	assert.Empty(t, recs)
}

func TestSweep_deletesOldFiles(t *testing.T) {
	p := paths.FromHome(t.TempDir())
	require.NoError(t, os.MkdirAll(p.InvocationsDir(), 0o755))

	keep := p.InvocationsFile("2026-06-25")
	stale := p.InvocationsFile("2026-05-20")
	require.NoError(t, os.WriteFile(keep, []byte(`{"invocation_id":"k"}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(stale, []byte(`{"invocation_id":"s"}`+"\n"), 0o644))

	now := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	removed, err := Sweep(p, 30*24*time.Hour, now)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	_, err = os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "stale file should be removed")
	_, err = os.Stat(keep)
	assert.NoError(t, err, "recent file should be preserved")
}

func TestSweep_ignoresUnrecognisedFilenames(t *testing.T) {
	p := paths.FromHome(t.TempDir())
	require.NoError(t, os.MkdirAll(p.InvocationsDir(), 0o755))

	noise := filepath.Join(p.InvocationsDir(), "not-a-date.ndjson")
	require.NoError(t, os.WriteFile(noise, []byte(""), 0o644))

	removed, err := Sweep(p, 30*24*time.Hour, time.Now().UTC())
	require.NoError(t, err)
	assert.Equal(t, 0, removed)

	_, err = os.Stat(noise)
	assert.NoError(t, err)
}

func TestRecord_jsonRoundtrip(t *testing.T) {
	start := time.Date(2026, 6, 25, 13, 14, 15, 0, time.UTC)
	rec := Record{
		InvocationID: "inv-42",
		Command:      "xcodebuild -workspace Foo.xcworkspace -scheme Foo",
		Tool:         ToolXcode,
		ToolVersion:  "16.0",
		CLIVersion:   "v2.8.6",
		StartedAt:    start,
		FinishedAt:   start.Add(45 * time.Second),
		ExitCode:     0,
		Source:       SourceCI,
	}

	b, err := json.Marshal(rec)
	require.NoError(t, err)

	var got Record
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, rec, got)
}

func TestRecord_finishedAtOmittedWhenZero(t *testing.T) {
	rec := Record{InvocationID: "i", StartedAt: time.Now().UTC()}

	b, err := json.Marshal(rec)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "finished_at", "zero FinishedAt should be omitted via omitzero")
}

func TestSweep_emptyDirIsNoop(t *testing.T) {
	p := paths.FromHome(t.TempDir())

	removed, err := Sweep(p, 30*24*time.Hour, time.Now().UTC())
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
}

func TestWriter_Append_readOnlyDirSurfacesError(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.InvocationsDir(), 0o755))
	require.NoError(t, os.Chmod(p.InvocationsDir(), 0o500))

	t.Cleanup(func() { _ = os.Chmod(p.InvocationsDir(), 0o755) })

	w := NewWriter(p)
	w.Clock = func() time.Time { return time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC) }

	err := w.Append(Record{InvocationID: "ro"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open invocation log")
}

func TestReader_Recent_skipsMalformedLines(t *testing.T) {
	p := paths.FromHome(t.TempDir())
	require.NoError(t, os.MkdirAll(p.InvocationsDir(), 0o755))

	day := p.InvocationsFile("2026-06-25")
	body := "{not json}\n" + `{"invocation_id":"good"}` + "\n"
	require.NoError(t, os.WriteFile(day, []byte(body), 0o644))

	r := NewReader(p)
	recs, err := r.Recent(10)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "good", recs[0].InvocationID)
}

func TestWriter_Append_runsSweepAfterMarkerExpiry(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	w, p := newTestWriter(t, now)

	stale := p.InvocationsFile("2026-04-01")
	require.NoError(t, os.MkdirAll(p.InvocationsDir(), 0o755))
	require.NoError(t, os.WriteFile(stale, []byte(`{"invocation_id":"old"}`+"\n"), 0o644))

	require.NoError(t, w.Append(Record{InvocationID: "fresh"}))

	_, err := os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "stale file should have been swept")
}

// Golden cross-writer parity: encoding the canonical Record on the Go side
// must produce the same bytes as docs/canonical-record.ndjson. The gradle
// plugin's LocalInvocationLogTest holds the same assertion on the Kotlin
// side. Schema drift between the two writers fails one or both tests.
func TestCanonicalRecord_byteEqualToCheckedInFixture(t *testing.T) {
	fixture, err := os.ReadFile(canonicalRecordPath)
	require.NoError(t, err)

	rec := Record{
		InvocationID: "cafe-1234",
		Command:      "xcodebuild build -workspace Foo.xcworkspace -scheme Foo",
		Tool:         ToolXcode,
		ToolVersion:  "16.0",
		CLIVersion:   "v2.8.6",
		StartedAt:    time.Date(2026, 6, 25, 13, 14, 15, 0, time.UTC),
		FinishedAt:   time.Date(2026, 6, 25, 13, 15, 0, 0, time.UTC),
		ExitCode:     0,
		Source:       SourceLocal,
	}

	encoded, err := encodeRecord(rec)
	require.NoError(t, err)
	assert.Equal(t, string(fixture), string(encoded), "canonical-record.ndjson must round-trip through the Go writer byte-identically")
}
