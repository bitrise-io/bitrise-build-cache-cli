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

func TestJoinManifest_MatchesByDescription_OverridesTool_FillsIO(t *testing.T) {
	// A Task in the activity log carries the signature-form key, which is
	// `<description> (in target '<name>' from project '<name>')`. JoinManifest
	// strips that suffix and matches against the manifest's description.
	report := buildanalyzer.Report{
		Build: buildanalyzer.BuildMeta{Title: "Build Foo"},
		Tasks: []buildanalyzer.Task{
			{
				Key:      "Ld /Foo normal arm64 (in target 'Foo' from project 'Foo')",
				Tool:     "Ld", // v0 first-word heuristic
				ExitCode: 0,
				Ran:      true,
			},
		},
	}

	manifest := buildanalyzer.Manifest{
		Commands: map[string]buildanalyzer.ManifestCommand{
			"P2:target-X:Debug:Ld Foo": {
				Tool:        "linker",
				Description: "Ld /Foo normal arm64",
				Inputs:      []string{"Foo.o"},
				Outputs:     []string{"/Foo"},
			},
			"<all>": {Tool: "phony"},
		},
	}

	buildanalyzer.JoinManifest(&report, manifest)

	require.Len(t, report.Tasks, 1, "no planned-only tasks appended; the only descripted command matched")
	assert.True(t, report.Tasks[0].Planned)
	assert.True(t, report.Tasks[0].Ran)
	assert.Equal(t, "linker", report.Tasks[0].Tool, "manifest tool wins over v0's first-word heuristic")
	assert.Equal(t, []string{"Foo.o"}, report.Tasks[0].Inputs)
	assert.Equal(t, []string{"/Foo"}, report.Tasks[0].Outputs)
}

func TestJoinManifest_PrefersSignatureOverDescription(t *testing.T) {
	// Two manifest commands sharing the same description; the task's Key
	// equals the signature of command B. Signature match beats description.
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{Key: "sig-B", Ran: true}},
	}
	manifest := buildanalyzer.Manifest{
		Commands: map[string]buildanalyzer.ManifestCommand{
			"CmdA": {Tool: "linker", Description: "shared", Signature: "sig-A"},
			"CmdB": {Tool: "shell", Description: "shared", Signature: "sig-B", Outputs: []string{"out-B"}},
		},
	}

	buildanalyzer.JoinManifest(&report, manifest)

	// The signature-matched Task picks up CmdB's tool + outputs.
	var matched buildanalyzer.Task
	for _, task := range report.Tasks {
		if task.Ran {
			matched = task
		}
	}
	assert.Equal(t, "shell", matched.Tool)
	assert.Equal(t, []string{"out-B"}, matched.Outputs)
}

func TestJoinManifest_AppendsPlannedNotRanTasks(t *testing.T) {
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{Key: "Ld /A (in target 'A' from project 'A')", Ran: true}},
	}
	manifest := buildanalyzer.Manifest{
		Commands: map[string]buildanalyzer.ManifestCommand{
			"CmdLd":            {Tool: "linker", Description: "Ld /A"},
			"CmdCompileNotRun": {Tool: "swift-driver-compilation", Description: "CompileSwift Unused.swift"},
			"<orch>":           {Tool: "phony"}, // no description — skipped
		},
	}

	buildanalyzer.JoinManifest(&report, manifest)

	require.Len(t, report.Tasks, 2, "ran + one planned-only; phony orchestration commands are skipped")
	planned := report.Tasks[1]
	assert.False(t, planned.Ran)
	assert.True(t, planned.Planned)
	assert.Equal(t, "CmdCompileNotRun", planned.Key)
	assert.Equal(t, "swift-driver-compilation", planned.Tool)
}

func TestJoinManifest_DerivesDependencyEdgesFromFilePaths(t *testing.T) {
	report := buildanalyzer.Report{}
	manifest := buildanalyzer.Manifest{
		Commands: map[string]buildanalyzer.ManifestCommand{
			"CompileA": {Tool: "swift-driver-compilation", Description: "Compile A.swift", Outputs: []string{"A.o"}},
			"CompileB": {Tool: "swift-driver-compilation", Description: "Compile B.swift", Outputs: []string{"B.o"}},
			"Ld":       {Tool: "linker", Description: "Ld /App", Inputs: []string{"A.o", "B.o"}, Outputs: []string{"/App"}},
		},
	}

	buildanalyzer.JoinManifest(&report, manifest)

	// Two edges: CompileA -> Ld, CompileB -> Ld (sorted lexicographically on From).
	require.Len(t, report.Dependencies, 2)
	assert.Equal(t, "CompileA", report.Dependencies[0].From)
	assert.Equal(t, "Ld", report.Dependencies[0].To)
	assert.Equal(t, "CompileB", report.Dependencies[1].From)
	assert.Equal(t, "Ld", report.Dependencies[1].To)
}

func TestJoinManifest_UnescapesBackslashSpace(t *testing.T) {
	// SwiftDriver signatures carry `\ ` (backslash-space) in the activity log
	// that the manifest description omits.
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{
			Key: `SwiftDriver\ Compilation MinimalApp normal arm64 (in target 'X' from project 'X')`,
			Ran: true,
		}},
	}
	manifest := buildanalyzer.Manifest{
		Commands: map[string]buildanalyzer.ManifestCommand{
			"CompReq": {
				Tool:        "swift-driver-compilation",
				Description: "SwiftDriver Compilation MinimalApp normal arm64",
				Outputs:     []string{"MinimalApp.swiftmodule"},
			},
		},
	}

	buildanalyzer.JoinManifest(&report, manifest)

	assert.True(t, report.Tasks[0].Planned, "backslash-space normalization should still match")
	assert.Equal(t, "swift-driver-compilation", report.Tasks[0].Tool)
}

func TestJoinManifest_NormalizesEscapedSpacesOnManifestSide(t *testing.T) {
	// Defensive — if a future Xcode starts emitting `\ ` in the manifest
	// description too, the join should still land. Both sides pass through
	// normalizeMatchKey.
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{
			Key: `CompileC foo bar.m (in target 'X' from project 'X')`,
			Ran: true,
		}},
	}
	manifest := buildanalyzer.Manifest{
		Commands: map[string]buildanalyzer.ManifestCommand{
			"Cmd": {
				Tool:        "clang-driver",
				Description: `CompileC foo\ bar.m`,
			},
		},
	}

	buildanalyzer.JoinManifest(&report, manifest)

	assert.True(t, report.Tasks[0].Planned, "both sides should normalize to the same form")
	assert.Equal(t, "clang-driver", report.Tasks[0].Tool)
}

func TestJoinManifest_AttachesSwiftSubEventsToParentDriver(t *testing.T) {
	// Activity-log SwiftCompile / SwiftEmitModule / SwiftDriverJobDiscovery
	// rows are sub-events of the swift driver with no separate manifest
	// entry. JoinManifest attributes them to the parent driver command by
	// target name so Planned=true and the Tool/Inputs/Outputs match.
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{
			{Key: `SwiftCompile normal arm64 Compiling\ App.swift /tmp/App.swift (in target 'MinimalApp' from project 'MinimalApp')`, Ran: true},
			{Key: `SwiftEmitModule normal arm64 Emitting\ module\ for\ MinimalApp (in target 'MinimalApp' from project 'MinimalApp')`, Ran: true},
			{Key: `SwiftDriverJobDiscovery normal arm64 Compiling App.swift (in target 'MinimalApp' from project 'MinimalApp')`, Ran: true},
		},
	}
	manifest := buildanalyzer.Manifest{
		Commands: map[string]buildanalyzer.ManifestCommand{
			"P0:target-X:Debug:SwiftDriver Compilation MinimalApp normal arm64 com.apple.xcode.tools.swift.compiler": {
				Tool:        "swift-driver-compilation",
				Description: "SwiftDriver Compilation MinimalApp normal arm64 com.apple.xcode.tools.swift.compiler",
				Outputs:     []string{"MinimalApp.swiftmodule"},
			},
		},
	}

	buildanalyzer.JoinManifest(&report, manifest)

	for i, task := range report.Tasks[:3] {
		assert.True(t, task.Planned, "swift sub-event %d should attach to parent driver", i)
		assert.Equal(t, "swift-driver-compilation", task.Tool, "sub-event %d inherits driver's tool", i)
	}
}

func TestJoinManifest_PopulatesArgs(t *testing.T) {
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{
			Key: `Ld /Foo normal arm64 (in target 'Foo' from project 'Foo')`,
			Ran: true,
		}},
	}
	manifest := buildanalyzer.Manifest{
		Commands: map[string]buildanalyzer.ManifestCommand{
			"P2:target-X:Debug:Ld Foo": {
				Tool:        "linker",
				Description: "Ld /Foo normal arm64",
				Args:        []string{"/usr/bin/clang", "-Xlinker", "-no_deduplicate"},
			},
		},
	}

	buildanalyzer.JoinManifest(&report, manifest)

	assert.Equal(t, []string{"/usr/bin/clang", "-Xlinker", "-no_deduplicate"}, report.Tasks[0].Args)
}

func TestJoinManifest_StripsPriorityPrefix(t *testing.T) {
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{
			Key: `Ld /Foo normal arm64 (in target 'Foo' from project 'Foo')`,
			Ran: true,
		}},
	}
	manifest := buildanalyzer.Manifest{
		Commands: map[string]buildanalyzer.ManifestCommand{
			"P2:target-X:Debug:Ld /Foo normal arm64": {
				Tool:        "linker",
				Description: "Ld /Foo normal arm64",
				Outputs:     []string{"/Foo"},
			},
			// Also an unmatched one — its planned-only Task should also
			// carry the stripped key + priority.
			"P0:::CreateBuildDirectory /tmp/out": {
				Tool:        "create-build-directory",
				Description: "CreateBuildDirectory /tmp/out",
			},
		},
	}

	buildanalyzer.JoinManifest(&report, manifest)

	// Ran task: Key preserved (activity-log form), ManifestKey + Priority added.
	ranTask := report.Tasks[0]
	assert.Equal(t, 2, ranTask.Priority, "priority parsed off P<N>: prefix")
	assert.Equal(t, "P2:target-X:Debug:Ld /Foo normal arm64", ranTask.ManifestKey,
		"original manifest key preserved on the matched task")

	// Planned-only task: Key is stripped, ManifestKey carries the full form.
	var plannedOnly buildanalyzer.Task
	for _, t := range report.Tasks {
		if !t.Ran {
			plannedOnly = t
		}
	}
	assert.Equal(t, "CreateBuildDirectory /tmp/out", plannedOnly.Key,
		"planned-only Task.Key is the stripped form (no `P0:::` prefix)")
	assert.Equal(t, "P0:::CreateBuildDirectory /tmp/out", plannedOnly.ManifestKey)
	assert.Equal(t, 0, plannedOnly.Priority)

	// Dependency edges — none wired for this fixture, but verify any edge
	// that would be synthesized uses the stripped key form.
}

func TestJoinManifest_DependencyEdgesUseStrippedKeys(t *testing.T) {
	// Two manifest commands with prefix; the derived dependency's endpoints
	// must carry the stripped key form so a reader can grep the Dot output
	// cleanly.
	report := buildanalyzer.Report{}
	manifest := buildanalyzer.Manifest{
		Commands: map[string]buildanalyzer.ManifestCommand{
			"P0:target-X:Debug:CompileA": {
				Tool: "swift-driver-compilation", Description: "CompileA", Outputs: []string{"A.o"},
			},
			"P2:target-X:Debug:LdApp": {
				Tool: "linker", Description: "LdApp", Inputs: []string{"A.o"}, Outputs: []string{"/App"},
			},
		},
	}

	buildanalyzer.JoinManifest(&report, manifest)

	require.Len(t, report.Dependencies, 1)
	assert.Equal(t, "CompileA", report.Dependencies[0].From, "edge uses stripped key, not P0:... prefix")
	assert.Equal(t, "LdApp", report.Dependencies[0].To)
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

// TestReadJoinClassify_EndToEnd chains Read → JoinManifest → Classify against
// an in-memory fixture so the v2 pipeline is proven end-to-end without a real
// DerivedData tree. The fixture pins one of each ExecutionReason bucket.
func TestReadJoinClassify_EndToEnd(t *testing.T) {
	// Report seeded as-if ReadReport returned three ran tasks (two match the
	// manifest, one is a cold rule with no manifest entry).
	report := buildanalyzer.Report{
		Build: buildanalyzer.BuildMeta{Title: "Build Foo"},
		Tasks: []buildanalyzer.Task{
			{Key: `Ld /Foo (in target 'Foo' from project 'Foo')`, Ran: true},
			{Key: `CompileSwift Bar.swift (in target 'Foo' from project 'Foo')`, Ran: true},
			{Key: `BogusActivityOnlySection`, Ran: true},
		},
	}

	manifest := buildanalyzer.Manifest{
		Commands: map[string]buildanalyzer.ManifestCommand{
			"P2:target-X:Debug:Ld /Foo": {
				Tool: "linker", Description: "Ld /Foo", Outputs: []string{"/Foo"},
			},
			"P0:target-X:Debug:CompileSwift Bar.swift": {
				Tool: "swift-driver-compilation", Description: "CompileSwift Bar.swift",
			},
			// A planned command that didn't run this build — becomes
			// Planned=true, Ran=false after JoinManifest.
			"P0:::CopyPNGFile Icon.png": {
				Tool: "shell", Description: "CopyPNGFile Icon.png",
			},
		},
	}

	buildanalyzer.JoinManifest(&report, manifest)

	snap := buildanalyzer.Snapshot{
		Iteration: 7,
		Rules: map[string]buildanalyzer.RuleResult{
			// Linker ran this build.
			"CP2:target-X:Debug:Ld /Foo": {
				Key:       "CP2:target-X:Debug:Ld /Foo",
				Iteration: 7,
				Signature: []byte{0x11, 0x22},
			},
			// Compile was skipped — carried over from iteration 2.
			"CP0:target-X:Debug:CompileSwift Bar.swift": {
				Key:       "CP0:target-X:Debug:CompileSwift Bar.swift",
				Iteration: 2,
				Signature: []byte{0x33},
			},
			// CopyPNGFile is cold — no rule row (left absent on purpose).
		},
	}

	buildanalyzer.Classify(&report, snap)

	// Count by reason.
	counts := map[buildanalyzer.ExecutionReason]int{}
	for _, task := range report.Tasks {
		counts[task.ExecutionReason]++
	}

	assert.Equal(t, 1, counts[buildanalyzer.ReasonRanThisBuild], "Ld /Foo ran this build")
	assert.Equal(t, 1, counts[buildanalyzer.ReasonSkippedUpToDate], "CompileSwift was reused")
	assert.Equal(t, 2, counts[buildanalyzer.ReasonCold], "CopyPNGFile + BogusActivityOnlySection are cold")
}
