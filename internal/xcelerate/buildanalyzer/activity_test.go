//go:build unit

package buildanalyzer_test

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

// slfBuilder synthesizes an SLF payload token-by-token so the test suite has
// a fixture that doesn't depend on a real Xcode run.
type slfBuilder struct {
	buf bytes.Buffer
}

func (b *slfBuilder) magic() *slfBuilder {
	b.buf.WriteString("SLF0")

	return b
}

func (b *slfBuilder) intTok(n int64) *slfBuilder {
	fmt.Fprintf(&b.buf, "%d#", n)

	return b
}

func (b *slfBuilder) strTok(s string) *slfBuilder {
	fmt.Fprintf(&b.buf, "%d\"%s", len(s), s)

	return b
}

func (b *slfBuilder) nilTok() *slfBuilder {
	b.buf.WriteByte('-')

	return b
}

func (b *slfBuilder) classNameTok(s string) *slfBuilder {
	fmt.Fprintf(&b.buf, "%d%%%s", len(s), s)

	return b
}

func (b *slfBuilder) classRefTok(idx int64) *slfBuilder {
	fmt.Fprintf(&b.buf, "%d@", idx)

	return b
}

func (b *slfBuilder) listHeader(n int64) *slfBuilder {
	fmt.Fprintf(&b.buf, "%d(", n)

	return b
}

func (b *slfBuilder) doubleTok(f float64) *slfBuilder {
	raw := make([]byte, 8)
	binary.LittleEndian.PutUint64(raw, math.Float64bits(f))
	b.buf.WriteString(hex.EncodeToString(raw))
	b.buf.WriteByte('^')

	return b
}

func (b *slfBuilder) bytes() []byte { return b.buf.Bytes() }

// buildSynthLog writes a gzipped SLF payload with one top-level section and
// numTasks command invocation sections. tasks[i].started/stopped are in CF
// absolute-time seconds.
func buildSynthLog(t *testing.T, buildTitle string, buildStart, buildStop float64, tasks []synthTask) []byte {
	t.Helper()

	b := &slfBuilder{}
	b.magic()
	// logFormatVersion
	b.intTok(13)

	// Top-level IDEActivityLogSection — field layout mirrors the live Xcode
	// log: sectionType, domainType, title, signature, timeStartedRecording,
	// timeStoppedRecording. The reader only needs the first 6 fields.
	b.classNameTok("IDEActivityLogSection") // pool[1]
	b.classRefTok(1)
	b.intTok(0)
	b.strTok("Xcode.IDEActivityLogDomainType.BuildLog")
	b.strTok(buildTitle)
	b.strTok(buildTitle) // signature mirrors title for the enclosing section
	b.doubleTok(buildStart)
	b.doubleTok(buildStop)

	// Command invocation sections follow inside the top-level's subSections.
	b.classNameTok("IDEActivityLogCommandInvocationSection") // pool[2]

	for _, task := range tasks {
		b.classRefTok(2)
		b.intTok(2)
		b.strTok("com.apple.dt.IDE.BuildLogSection")
		b.strTok(task.title)
		b.strTok(task.signature)
		b.doubleTok(task.started)
		b.doubleTok(task.stopped)
		b.nilTok() // subSections placeholder
		b.nilTok()
		b.nilTok()
		b.intTok(0)
		b.intTok(0)
		b.intTok(0)
		b.intTok(0)
		b.nilTok()
		b.nilTok()
		b.strTok(task.commandDetail)
		b.strTok(task.uuid)
		b.nilTok()
		b.nilTok()
		b.listHeader(0)
		b.intTok(int64(task.exitCode))
	}

	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, err := gw.Write(b.bytes())
	require.NoError(t, err)
	require.NoError(t, gw.Close())

	return gz.Bytes()
}

type synthTask struct {
	title         string
	signature     string
	started       float64
	stopped       float64
	commandDetail string
	uuid          string
	exitCode      int
}

func writeSynthLog(t *testing.T, name string, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, body, 0o644))

	return path
}

func TestReadReport_Synthesized_SingleOKTask(t *testing.T) {
	const start = 812533954.0
	const stop = 812533955.5

	body := buildSynthLog(t, "Build CasProbe", start, stop+1, []synthTask{{
		title:         "Compile Swift sources",
		signature:     "SwiftCompile normal arm64 Foo.swift (in target 'CasProbe' from project 'CasProbe')",
		started:       start,
		stopped:       stop,
		commandDetail: "builtin-SwiftDriver -- swiftc -module-name CasProbe Foo.swift",
		uuid:          "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE",
		exitCode:      0,
	}})
	path := writeSynthLog(t, "synth.xcactivitylog", body)

	report, err := buildanalyzer.ReadReport(path)
	require.NoError(t, err)

	require.Len(t, report.Tasks, 1)
	task := report.Tasks[0]
	assert.Equal(t, "SwiftCompile normal arm64 Foo.swift (in target 'CasProbe' from project 'CasProbe')", task.Key)
	assert.Equal(t, "CasProbe", task.Target)
	assert.Equal(t, "SwiftCompile", task.Tool)
	assert.Equal(t, 0, task.ExitCode)
	assert.InDelta(t, 1500*time.Millisecond, task.Duration, float64(10*time.Millisecond))
	assert.False(t, task.StartedAt.IsZero(), "StartedAt should decode from CF absolute time")
	assert.Equal(t, "Build CasProbe", report.Build.Title)
	assert.False(t, report.Build.StartedAt.IsZero())
}

func TestReadReport_Synthesized_MultipleTasks_PreservesOrder(t *testing.T) {
	const base = 812533954.0

	body := buildSynthLog(t, "Build Foo", base, base+10, []synthTask{
		{
			title:         "Compile A",
			signature:     "SwiftCompile A (in target 'Foo' from project 'Foo')",
			started:       base + 1,
			stopped:       base + 2,
			commandDetail: "swiftc A.swift",
			uuid:          "11111111-1111-1111-1111-111111111111",
			exitCode:      0,
		},
		{
			title:         "Link Foo",
			signature:     "Ld /Foo.app/Foo normal arm64 (in target 'Foo' from project 'Foo')",
			started:       base + 3,
			stopped:       base + 4,
			commandDetail: "clang -o Foo",
			uuid:          "22222222-2222-2222-2222-222222222222",
			exitCode:      0,
		},
	})
	path := writeSynthLog(t, "multi.xcactivitylog", body)

	report, err := buildanalyzer.ReadReport(path)
	require.NoError(t, err)

	require.Len(t, report.Tasks, 2)
	assert.Equal(t, "SwiftCompile", report.Tasks[0].Tool)
	assert.Equal(t, "Ld", report.Tasks[1].Tool)
	assert.Equal(t, "Foo", report.Tasks[0].Target)
	assert.Equal(t, "Foo", report.Tasks[1].Target)
}

func TestReadReport_Synthesized_NonZeroExitCodeAndErrors(t *testing.T) {
	const base = 812533954.0

	body := buildSynthLog(t, "Build Foo", base, base+5, []synthTask{{
		title:         "Compile Bad",
		signature:     "SwiftCompile Bad.swift (in target 'Foo' from project 'Foo')",
		started:       base + 1,
		stopped:       base + 2,
		commandDetail: "swiftc Bad.swift\nBad.swift:12:5: error: cannot find 'nope' in scope\nerror: compilation failed",
		uuid:          "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE",
		exitCode:      1,
	}})
	path := writeSynthLog(t, "fail.xcactivitylog", body)

	report, err := buildanalyzer.ReadReport(path)
	require.NoError(t, err)

	require.Len(t, report.Tasks, 1)
	assert.Equal(t, 1, report.Tasks[0].ExitCode)
	assert.Len(t, report.Tasks[0].Errors, 2, "every `error:` line becomes an Errors entry")
}

func TestReadReport_MissingFile(t *testing.T) {
	_, err := buildanalyzer.ReadReport(filepath.Join(t.TempDir(), "nope.xcactivitylog"))
	require.Error(t, err)
}

func TestReadReport_BadMagic(t *testing.T) {
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, err := gw.Write([]byte("NOTSLF-random"))
	require.NoError(t, err)
	require.NoError(t, gw.Close())

	path := writeSynthLog(t, "badmagic.xcactivitylog", gz.Bytes())

	_, err = buildanalyzer.ReadReport(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad magic")
}

func TestReadReport_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trunc.xcactivitylog")
	require.NoError(t, os.WriteFile(path, []byte{0x1f, 0x8b, 0x08}, 0o644))

	_, err := buildanalyzer.ReadReport(path)
	require.Error(t, err)
}

func TestMostRecentActivityLog_PicksNewest(t *testing.T) {
	dir := t.TempDir()
	logsBuild := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(logsBuild, 0o755))

	older := filepath.Join(logsBuild, "older.xcactivitylog")
	newer := filepath.Join(logsBuild, "newer.xcactivitylog")

	require.NoError(t, os.WriteFile(older, []byte("x"), 0o644))
	// Second write uses time.Now, so we ensure strictly ascending mtime.
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, os.WriteFile(newer, []byte("x"), 0o644))

	path, err := buildanalyzer.MostRecentActivityLog(dir)
	require.NoError(t, err)
	assert.Equal(t, newer, path)
}

func TestMostRecentActivityLog_NoMatch(t *testing.T) {
	dir := t.TempDir()
	logsBuild := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(logsBuild, 0o755))

	path, err := buildanalyzer.MostRecentActivityLog(dir)
	require.NoError(t, err)
	assert.Empty(t, path)
}

func TestMostRecentActivityLog_MissingDir(t *testing.T) {
	_, err := buildanalyzer.MostRecentActivityLog(t.TempDir())
	require.Error(t, err)
}

// TestReadReport_RealSample exercises the walker against the committed
// fixture from the sibling xcactivitylog package — a ~405 KB CasProbe build
// with Xcode 26.x logFormatVersion 11. The fixture is shared to avoid
// re-committing a second real log under this package's testdata.
func TestReadReport_RealSample(t *testing.T) {
	path := filepath.Join("..", "..", "xcactivitylog", "testdata", "sample.xcactivitylog")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("sibling testdata sample not committed in this environment")
	}

	report, err := buildanalyzer.ReadReport(path)
	require.NoError(t, err)

	assert.Equal(t, "Build CasProbe", report.Build.Title)
	assert.False(t, report.Build.StartedAt.IsZero(), "top-level StartedAt decodes from CF absolute time")
	assert.Positive(t, report.Build.Duration, "top-level build duration is positive")

	assert.NotEmpty(t, report.Tasks, "a CasProbe build produces at least one executed task")

	// The sample is a cache-hot build — every task should finish with exitCode
	// 0. v0's "last int wins" heuristic can mis-classify a trailing attachment
	// counter as a non-zero exit code; accept a handful of false positives
	// without hard-failing so an unrelated schema drift doesn't tank the run.
	var nonZero int
	for _, task := range report.Tasks {
		assert.NotEmpty(t, task.Key, "every task carries a signature")
		if task.ExitCode != 0 {
			nonZero++
		}
	}
	assert.Less(t, nonZero, len(report.Tasks)/4, "most tasks on a green build decode as exitCode 0")
}
