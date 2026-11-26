//go:build unit

package buildanalyzer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

func TestRun_HappyPath_LogOnly_NoManifestOrDB(t *testing.T) {
	const base = 812533954.0

	body := buildSynthLog(t, "Build RunHappy", base, base+2, []synthTask{{
		title:         "Compile Run",
		signature:     "SwiftCompile Run.swift (in target 'RunHappy' from project 'RunHappy')",
		started:       base,
		stopped:       base + 1,
		commandDetail: "swiftc Run.swift",
		uuid:          "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE",
		exitCode:      0,
	}})
	path := writeSynthLog(t, "run-happy.xcactivitylog", body)

	report, warnings, err := buildanalyzer.Run(context.Background(), path, buildanalyzer.RunOptions{
		DiagMode: buildanalyzer.DiagModeOff,
	})
	require.NoError(t, err)
	assert.Empty(t, warnings, "log-only standalone path produces no warnings")
	require.Len(t, report.Tasks, 1)
	assert.Equal(t, "RunHappy", report.Tasks[0].Target)
}

func TestRun_LogOnlyDegrade_ManifestReadErrorIsWarning(t *testing.T) {
	const base = 812533954.0

	body := buildSynthLog(t, "Build Degrade", base, base+2, []synthTask{{
		title:         "Compile Degrade",
		signature:     "SwiftCompile D.swift (in target 'Degrade' from project 'Degrade')",
		started:       base,
		stopped:       base + 1,
		commandDetail: "swiftc D.swift",
		uuid:          "BBBBBBBB-BBBB-CCCC-DDDD-EEEEEEEEEEEE",
		exitCode:      0,
	}})
	path := writeSynthLog(t, "degrade.xcactivitylog", body)

	// Point ExplicitManifest at a file that does not exist: Run must NOT error.
	report, warnings, err := buildanalyzer.Run(context.Background(), path, buildanalyzer.RunOptions{
		ExplicitManifest: filepath.Join(t.TempDir(), "missing-manifest.json"),
		ExplicitBuildDB:  filepath.Join(t.TempDir(), "missing-build.db"),
		DiagMode:         buildanalyzer.DiagModeOff,
	})
	require.NoError(t, err)
	require.Len(t, report.Tasks, 1, "log-only output is preserved")

	// Two warnings expected: manifest read + build.db read.
	require.GreaterOrEqual(t, len(warnings), 1, "manifest / db read failures land as warnings")
	for _, w := range warnings {
		assert.NotNil(t, w)
	}
}

func TestRun_CtxCancelBetweenStages_PropagatesError(t *testing.T) {
	const base = 812533954.0

	body := buildSynthLog(t, "Build CtxCancel", base, base+2, []synthTask{{
		title:         "Compile CtxCancel",
		signature:     "SwiftCompile C.swift (in target 'CtxCancel' from project 'CtxCancel')",
		started:       base,
		stopped:       base + 1,
		commandDetail: "swiftc C.swift",
		uuid:          "CCCCCCCC-BBBB-CCCC-DDDD-EEEEEEEEEEEE",
		exitCode:      0,
	}})
	path := writeSynthLog(t, "ctx-cancel.xcactivitylog", body)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := buildanalyzer.Run(ctx, path, buildanalyzer.RunOptions{
		DiagMode: buildanalyzer.DiagModeOff,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRun_EmptyLogPathErrors(t *testing.T) {
	_, _, err := buildanalyzer.Run(context.Background(), "", buildanalyzer.RunOptions{})
	require.Error(t, err)
}

func TestRun_UnreadableLogErrors(t *testing.T) {
	_, _, err := buildanalyzer.Run(context.Background(), filepath.Join(t.TempDir(), "missing.xcactivitylog"), buildanalyzer.RunOptions{})
	require.Error(t, err)
}

func TestRun_PopulatesSchemeAndCommandFromOpts(t *testing.T) {
	const base = 812533954.0

	body := buildSynthLog(t, "Prepare packages", base, base+2, []synthTask{{
		title:         "Compile Scheme",
		signature:     "SwiftCompile S.swift (in target 'SchemeOpt' from project 'SchemeOpt')",
		started:       base,
		stopped:       base + 1,
		commandDetail: "swiftc S.swift",
		uuid:          "EEEEEEEE-BBBB-CCCC-DDDD-EEEEEEEEEEEE",
		exitCode:      0,
	}})
	path := writeSynthLog(t, "scheme-opts.xcactivitylog", body)

	tests := []struct {
		name        string
		opts        buildanalyzer.RunOptions
		wantScheme  string
		wantCommand string
	}{
		{
			name:        "scheme+command supplied overrides empty meta",
			opts:        buildanalyzer.RunOptions{Scheme: "MyScheme", Command: "build [MyScheme / Debug]", DiagMode: buildanalyzer.DiagModeOff},
			wantScheme:  "MyScheme",
			wantCommand: "build [MyScheme / Debug]",
		},
		{
			name:        "scheme only",
			opts:        buildanalyzer.RunOptions{Scheme: "MyScheme", DiagMode: buildanalyzer.DiagModeOff},
			wantScheme:  "MyScheme",
			wantCommand: "",
		},
		{
			name:        "command only",
			opts:        buildanalyzer.RunOptions{Command: "build", DiagMode: buildanalyzer.DiagModeOff},
			wantScheme:  "",
			wantCommand: "build",
		},
		{
			name:        "neither set — fields stay empty, Title unchanged",
			opts:        buildanalyzer.RunOptions{DiagMode: buildanalyzer.DiagModeOff},
			wantScheme:  "",
			wantCommand: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report, _, err := buildanalyzer.Run(context.Background(), path, tc.opts)
			require.NoError(t, err)
			assert.Equal(t, tc.wantScheme, report.Build.Scheme, "scheme must mirror opts.Scheme")
			assert.Equal(t, tc.wantCommand, report.Build.Command, "command must mirror opts.Command")
			assert.Equal(t, "Prepare packages", report.Build.Title, "SLF-parsed Title stays untouched regardless of opts")
		})
	}
}

func TestRun_ClassifierBailIsWarning_NotFatal(t *testing.T) {
	const base = 812533954.0

	body := buildSynthLog(t, "Build ClassifyBail", base, base+2, []synthTask{{
		title:         "Compile C",
		signature:     "SwiftCompile C.swift (in target 'ClassifyBail' from project 'ClassifyBail')",
		started:       base,
		stopped:       base + 1,
		commandDetail: "swiftc C.swift",
		uuid:          "DDDDDDDD-BBBB-CCCC-DDDD-EEEEEEEEEEEE",
		exitCode:      0,
	}})
	path := writeSynthLog(t, "classify-bail.xcactivitylog", body)

	bogusDB := filepath.Join(t.TempDir(), "bogus.db")
	require.NoError(t, os.WriteFile(bogusDB, []byte("not a sqlite file"), 0o644))

	report, warnings, err := buildanalyzer.Run(context.Background(), path, buildanalyzer.RunOptions{
		ExplicitBuildDB: bogusDB,
		DisableSnapshot: true,
		DiagMode:        buildanalyzer.DiagModeOff,
	})
	require.NoError(t, err)
	assert.Len(t, report.Tasks, 1)
	require.NotEmpty(t, warnings, "garbled build.db must warn, not fatal")
}
