//go:build unit

package buildanalyzer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

func TestAttachDiagnostics_NilReport_NoPanic(t *testing.T) {
	// Defensive: downstream callers may pass nil when the SLF reader failed
	// to produce a report. AttachDiagnostics must stay a no-op.
	buildanalyzer.AttachDiagnostics(nil, "/tmp/dd", buildanalyzer.DiagModeAuto)
}

func TestAttachDiagnostics_Off_LeavesDiagnosticsUntouched(t *testing.T) {
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{
			Key:           "SwiftCompile App.swift",
			StderrExcerpt: "App.swift:5:9: error: cannot find 'Foo' in scope\n",
		}},
	}

	buildanalyzer.AttachDiagnostics(&report, "", buildanalyzer.DiagModeOff)

	assert.Empty(t, report.Tasks[0].Diagnostics)
}

func TestAttachDiagnostics_Auto_UsesStderrPath(t *testing.T) {
	// Pass bar when real activity logs come back with empty stderr:
	// synthesize a Task carrying a swiftc diagnostic blob and confirm the
	// extractor surfaces one structured record.
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{
			{
				Key:           "SwiftCompile normal arm64 Compiling App.swift",
				Ran:           true,
				ExitCode:      2,
				StderrExcerpt: "App.swift:5:9: error: cannot find 'Foo' in scope\nlet x = Foo()\n        ^~~\n",
			},
			{
				Key:           "CompileC main.m",
				Ran:           true,
				ExitCode:      0,
				StderrExcerpt: "",
			},
		},
	}

	buildanalyzer.AttachDiagnostics(&report, "/tmp/derived", buildanalyzer.DiagModeAuto)

	require.Len(t, report.Tasks[0].Diagnostics, 1)
	d := report.Tasks[0].Diagnostics[0]
	assert.Equal(t, "App.swift", d.File)
	assert.Equal(t, 5, d.Line)
	assert.Equal(t, 9, d.Column)
	assert.Equal(t, buildanalyzer.SeverityError, d.Severity)
	assert.Contains(t, d.Message, "cannot find 'Foo' in scope")

	// Second task's empty stderr yields no diagnostics.
	assert.Empty(t, report.Tasks[1].Diagnostics)
}

func TestAttachDiagnostics_Stderr_ForcesStderrExtraction(t *testing.T) {
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{
			StderrExcerpt: "main.m:10:5: error: expected ';'\n",
		}},
	}

	buildanalyzer.AttachDiagnostics(&report, "", buildanalyzer.DiagModeStderr)

	require.Len(t, report.Tasks[0].Diagnostics, 1)
	assert.Equal(t, buildanalyzer.SeverityError, report.Tasks[0].Diagnostics[0].Severity)
}

func TestAttachDiagnostics_Dia_NoReader_SkipsSilently(t *testing.T) {
	// `.dia` reader is deferred in v3 — the mode is accepted but produces
	// no diagnostics. Documented in docs/xcode-build-analyzer.md.
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{
			StderrExcerpt: "main.m:10:5: error: expected ';'\n",
		}},
	}

	buildanalyzer.AttachDiagnostics(&report, "", buildanalyzer.DiagModeDia)

	assert.Empty(t, report.Tasks[0].Diagnostics,
		"dia mode is a no-op in v3; forcing it must NOT fall through to stderr")
}

func TestAttachDiagnostics_Idempotent(t *testing.T) {
	// Re-running overwrites rather than duplicating — the counters that
	// downstream consumers derive stay stable across multiple calls.
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{
			StderrExcerpt: "App.swift:1:1: warning: unused\n",
		}},
	}

	buildanalyzer.AttachDiagnostics(&report, "", buildanalyzer.DiagModeAuto)
	buildanalyzer.AttachDiagnostics(&report, "", buildanalyzer.DiagModeAuto)

	require.Len(t, report.Tasks[0].Diagnostics, 1)
}
