package xcode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

const (
	outputJSON   = "json"
	outputPretty = "pretty"
	outputDot    = "dot"
)

//nolint:gochecknoglobals
var (
	analyzeFlagActivityLog  string
	analyzeFlagDerivedData  string
	analyzeFlagManifest     string
	analyzeFlagBuildDB      string
	analyzeFlagOutput       string
	analyzeFlagDiagnostics  string
	analyzeFlagSnapshotRoot string
	analyzeFlagNoSnapshot   bool
)

//nolint:gochecknoglobals
var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Post-mortem of an .xcactivitylog — task list, durations, errors.",
	Long: `analyze reads a single Xcode .xcactivitylog and emits a flat list of every
executed command section with its signature (llbuild cache key), target,
duration, exit code, and any error: lines.

Pass --xcactivitylog to point at a specific log, or --derived-data to pick
the most recent Logs/Build/*.xcactivitylog under a DerivedData path. If both
flags are set, --xcactivitylog wins.

Phased plan (see docs/xcode-build-analyzer.md):
  v0 — .xcactivitylog → flat task list, JSON or pretty.
  v1 (this subcommand) — join against the manifest.json graph (planned tasks,
     input/output dependency edges) + Graphviz dot renderer.
  v2 — annotate execution reason via build.db signature diff.
  v3 — rich clang diagnostics via .dia parse.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runAnalyze(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	},
}

func init() {
	xcodeCommand.AddCommand(analyzeCmd)

	analyzeCmd.Flags().StringVar(&analyzeFlagActivityLog, "xcactivitylog", "", "Path to an .xcactivitylog file. Overrides --derived-data when both are set.")
	analyzeCmd.Flags().StringVar(&analyzeFlagDerivedData, "derived-data", "", "DerivedData path; the newest Logs/Build/*.xcactivitylog and matching XCBuildData/manifest.json are picked.")
	analyzeCmd.Flags().StringVar(&analyzeFlagManifest, "manifest", "", "Optional path to a manifest.json for planned-task graph + dependency DAG. Auto-discovered when --derived-data is set.")
	analyzeCmd.Flags().StringVar(&analyzeFlagBuildDB, "build-db", "", "Optional path to an llbuild build.db for execution-reason classification. Auto-discovered when --derived-data is set.")
	analyzeCmd.Flags().StringVar(&analyzeFlagOutput, "output", outputJSON, "Output format: `json` (default), `pretty`, or `dot`.")
	analyzeCmd.Flags().StringVar(&analyzeFlagDiagnostics, "diagnostics", string(buildanalyzer.DiagModeAuto), "Diagnostics source: `auto` (default), `dia`, `stderr`, or `off`. v3 ships with the `.dia` reader deferred — `auto` and `stderr` behave identically today.")
	analyzeCmd.Flags().StringVar(&analyzeFlagSnapshotRoot, "snapshot-root", "", "Override the build-analyzer snapshot store root (default: ~/.local/state/xcelerate/build-analyzer/snapshots, env BITRISE_BUILD_ANALYZER_SNAPSHOT_ROOT). B.2 picks the matching snapshot under this root for signature-diff.")
	analyzeCmd.Flags().BoolVar(&analyzeFlagNoSnapshot, "no-snapshot", false, "Force reading the live build.db even when a matching snapshot exists. Debugging switch for comparing current vs snapshot behavior.")
}

func runAnalyze(ctx context.Context, out, stderr io.Writer) error {
	path, err := resolveAnalyzeLogPath(analyzeFlagActivityLog, analyzeFlagDerivedData)
	if err != nil {
		return err
	}

	mode, err := parseDiagnosticsFlag(analyzeFlagDiagnostics)
	if err != nil {
		return err
	}

	report, warnings, err := buildanalyzer.Run(ctx, path, buildanalyzer.RunOptions{
		DerivedDataPath:   analyzeFlagDerivedData,
		ExplicitManifest:  analyzeFlagManifest,
		ExplicitBuildDB:   analyzeFlagBuildDB,
		DiagMode:          mode,
		SnapshotStoreRoot: analyzeFlagSnapshotRoot,
		DisableSnapshot:   analyzeFlagNoSnapshot,
	})
	if err != nil {
		return fmt.Errorf("analyze %s: %w", path, err)
	}

	writeAnalyzeWarnings(stderr, warnings)

	switch analyzeFlagOutput {
	case outputPretty:
		return renderPretty(out, report)
	case outputDot:
		if err := buildanalyzer.RenderDot(out, report); err != nil {
			return fmt.Errorf("render dot: %w", err)
		}

		return nil
	case outputJSON, "":
		return renderJSON(out, report)
	default:
		return fmt.Errorf("unknown --output %q (want json|pretty|dot)", analyzeFlagOutput)
	}
}

// writeAnalyzeWarnings prefixes each non-fatal stage warning with `analyze:`
// so the CLI keeps surfacing the soft-fail reason on stderr.
func writeAnalyzeWarnings(stderr io.Writer, warnings []error) {
	for _, w := range warnings {
		if w == nil {
			continue
		}

		fmt.Fprintln(stderr, "analyze: "+w.Error())
	}
}

func parseDiagnosticsFlag(flag string) (buildanalyzer.DiagMode, error) {
	if flag == "" {
		return buildanalyzer.DiagModeAuto, nil
	}

	mode := buildanalyzer.DiagMode(flag)
	switch mode {
	case buildanalyzer.DiagModeAuto,
		buildanalyzer.DiagModeDia,
		buildanalyzer.DiagModeStderr,
		buildanalyzer.DiagModeOff:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown --diagnostics %q (want auto|dia|stderr|off)", flag)
	}
}

// resolveAnalyzeLogPath enforces the "one of two flags required" contract
// and prefers --xcactivitylog when both are set.
func resolveAnalyzeLogPath(explicit, derivedData string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	if derivedData == "" {
		return "", fmt.Errorf("one of --xcactivitylog or --derived-data is required")
	}

	path, err := buildanalyzer.MostRecentActivityLog(derivedData)
	if err != nil {
		return "", fmt.Errorf("locate newest xcactivitylog under %s: %w", derivedData, err)
	}

	if path == "" {
		return "", fmt.Errorf("no .xcactivitylog found under %s/Logs/Build", derivedData)
	}

	return path, nil
}

func renderJSON(out io.Writer, report buildanalyzer.Report) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")

	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("render JSON: %w", err)
	}

	return nil
}

func renderPretty(out io.Writer, report buildanalyzer.Report) error {
	if _, err := fmt.Fprintf(out, "Build: %s  (%s → %s, %s)\n",
		report.Build.Title,
		formatTimestamp(report.Build.StartedAt.Format("15:04:05.000")),
		formatTimestamp(report.Build.FinishedAt.Format("15:04:05.000")),
		report.Build.Duration,
	); err != nil {
		return fmt.Errorf("write pretty build line: %w", err)
	}

	if _, err := fmt.Fprintf(out, "Tasks: %d\n\n", len(report.Tasks)); err != nil {
		return fmt.Errorf("write pretty task count: %w", err)
	}

	for _, task := range report.Tasks {
		if _, err := fmt.Fprintf(out, "%s %s %10s  %s  %s  %s  exit=%d  %s%s\n",
			rootCauseMarker(task.RootCause),
			task.StartedAt.Format("15:04:05.000"),
			task.Duration,
			reasonLetter(task.ExecutionReason),
			upstreamColumn(task),
			diagnosticsIcon(task.Diagnostics),
			task.ExitCode,
			truncate(task.Key, 160),
			signatureChangeSuffix(task.SignatureChanged),
		); err != nil {
			return fmt.Errorf("write pretty task line: %w", err)
		}

		if task.SignatureChanged && task.ValueDiffNote != "" {
			if _, err := fmt.Fprintf(out, "    Δ %s\n", task.ValueDiffNote); err != nil {
				return fmt.Errorf("write pretty value-diff line: %w", err)
			}
		}

		for _, errLine := range task.Errors {
			if _, err := fmt.Fprintf(out, "    ! %s\n", errLine); err != nil {
				return fmt.Errorf("write pretty error line: %w", err)
			}
		}

		for _, d := range task.Diagnostics {
			if _, err := fmt.Fprintf(out, "    %s %s:%d:%d  %s\n",
				severityGlyph(d.Severity), d.File, d.Line, d.Column,
				truncate(d.Message, 200),
			); err != nil {
				return fmt.Errorf("write pretty diagnostic line: %w", err)
			}
		}
	}

	if err := renderPrettyFileChanges(out, report.FileChanges); err != nil {
		return err
	}

	return nil
}

// renderPrettyFileChanges adds a compact footer section for FileChanges —
// one node key per block, byte-level diff note on a Δ line, and (when
// decoded) the semantic FileInfo diff on a φ line. Silent when the slice
// is empty so single-snapshot runs don't pay the column.
func renderPrettyFileChanges(out io.Writer, changes []buildanalyzer.FileChange) error {
	if len(changes) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(out, "\nFile changes: %d\n", len(changes)); err != nil {
		return fmt.Errorf("write pretty filechanges header: %w", err)
	}

	for _, fc := range changes {
		if _, err := fmt.Fprintf(out, "  %s → %s\n", fc.NodeKey, fc.DownstreamTaskKey); err != nil {
			return fmt.Errorf("write pretty filechange line: %w", err)
		}

		if fc.ValueDiffNote != "" {
			if _, err := fmt.Fprintf(out, "    Δ %s\n", fc.ValueDiffNote); err != nil {
				return fmt.Errorf("write pretty filechange value-diff: %w", err)
			}
		}

		if fc.SemanticDiff != "" {
			if _, err := fmt.Fprintf(out, "    φ %s\n", fc.SemanticDiff); err != nil {
				return fmt.Errorf("write pretty filechange semantic-diff: %w", err)
			}
		}
	}

	return nil
}

// diagnosticsIcon surfaces a `[E/W/N]` column per task: `E` when any
// error/fatal is attached, else `W` when a warning is present, else `N` when
// a note is present, else `-`. The order matters — a failing task with both
// warnings and errors reads as `E`.
func diagnosticsIcon(diags []buildanalyzer.Diagnostic) string {
	if len(diags) == 0 {
		return "-"
	}

	var hasErr, hasWarn, hasNote bool
	for _, d := range diags {
		switch d.Severity {
		case buildanalyzer.SeverityError, buildanalyzer.SeverityFatal:
			hasErr = true
		case buildanalyzer.SeverityWarning:
			hasWarn = true
		case buildanalyzer.SeverityNote:
			hasNote = true
		}
	}

	switch {
	case hasErr:
		return "E"
	case hasWarn:
		return "W"
	case hasNote:
		return "N"
	default:
		return "-"
	}
}

func severityGlyph(sev buildanalyzer.Severity) string {
	switch sev {
	case buildanalyzer.SeverityError, buildanalyzer.SeverityFatal:
		return "!"
	case buildanalyzer.SeverityWarning:
		return "~"
	case buildanalyzer.SeverityNote:
		return "i"
	default:
		return "?"
	}
}

func formatTimestamp(s string) string {
	if s == "" {
		return "-"
	}

	return s
}

// reasonLetter returns a single-character column shown before the exit code
// in --output=pretty: `C` cold, `R` ran this build, `S` skipped up-to-date,
// `U` unclassified, `N` new-task (B.2, prior snapshot present and task had no
// matching prior rule), `V` re-evaluated-cached (rule re-checked by llbuild
// but no command invoked — the activity log confirms no work was done),
// `-` when the field is unpopulated (no build.db joined).
func reasonLetter(reason buildanalyzer.ExecutionReason) string {
	switch reason {
	case buildanalyzer.ReasonCold:
		return "C"
	case buildanalyzer.ReasonRanThisBuild:
		return "R"
	case buildanalyzer.ReasonSkippedUpToDate:
		return "S"
	case buildanalyzer.ReasonUnclassified:
		return "U"
	case buildanalyzer.ReasonNewTask:
		return "N"
	case buildanalyzer.ReasonReEvaluatedCached:
		return "V"
	default:
		return "-"
	}
}

// rootCauseMarker returns `*` when a task is a root cause of the ran set, a
// single space otherwise — so the column occupies one glyph in both cases and
// the following columns stay aligned.
func rootCauseMarker(rootCause bool) string {
	if rootCause {
		return "*"
	}

	return " "
}

// signatureChangeSuffix returns a trailing ` *` marker (space + asterisk) on
// signature-changed tasks, empty string otherwise. Placed after the key so the
// column alignment above stays intact — the spec's "`*` on signature-changed
// tasks" lands here rather than fighting rootCauseMarker's own `*`.
func signatureChangeSuffix(changed bool) string {
	if changed {
		return " *"
	}

	return ""
}

// upstreamColumn renders `U=N` for a populated UpstreamRanCount and dashes
// otherwise, so every ran-this-build row carries a comparable width. Non-ran
// tasks (no attribution) read as `-----`.
func upstreamColumn(task buildanalyzer.Task) string {
	if task.ExecutionReason != buildanalyzer.ReasonRanThisBuild {
		return "-----"
	}

	return fmt.Sprintf("U=%-3d", task.UpstreamRanCount)
}
