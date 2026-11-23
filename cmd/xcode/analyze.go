package xcode

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

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
	analyzeFlagActivityLog string
	analyzeFlagDerivedData string
	analyzeFlagManifest    string
	analyzeFlagOutput      string
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
		return runAnalyze(cmd.OutOrStdout(), cmd.ErrOrStderr())
	},
}

func init() {
	xcodeCommand.AddCommand(analyzeCmd)

	analyzeCmd.Flags().StringVar(&analyzeFlagActivityLog, "xcactivitylog", "", "Path to an .xcactivitylog file. Overrides --derived-data when both are set.")
	analyzeCmd.Flags().StringVar(&analyzeFlagDerivedData, "derived-data", "", "DerivedData path; the newest Logs/Build/*.xcactivitylog and matching XCBuildData/manifest.json are picked.")
	analyzeCmd.Flags().StringVar(&analyzeFlagManifest, "manifest", "", "Optional path to a manifest.json for planned-task graph + dependency DAG. Auto-discovered when --derived-data is set.")
	analyzeCmd.Flags().StringVar(&analyzeFlagOutput, "output", outputJSON, "Output format: `json` (default), `pretty`, or `dot`.")
}

func runAnalyze(out, stderr io.Writer) error {
	path, err := resolveAnalyzeLogPath(analyzeFlagActivityLog, analyzeFlagDerivedData)
	if err != nil {
		return err
	}

	report, err := buildanalyzer.ReadReport(path)
	if err != nil {
		return fmt.Errorf("analyze %s: %w", path, err)
	}

	if err := joinManifestIfAvailable(&report, stderr, analyzeFlagManifest, analyzeFlagDerivedData); err != nil {
		return err
	}

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

// joinManifestIfAvailable resolves the manifest path with the following
// precedence: explicit --manifest flag wins; otherwise auto-discover via
// --derived-data. On auto-discover miss or explicit load failure, the
// analyzer falls back to v0 behavior with a stderr warning rather than
// erroring — the manifest is additive, not a hard requirement.
func joinManifestIfAvailable(report *buildanalyzer.Report, stderr io.Writer, explicit, derivedData string) error {
	path, err := resolveAnalyzeManifestPath(explicit, derivedData)
	if err != nil {
		return err
	}

	if path == "" {
		fmt.Fprintln(stderr, "analyze: no manifest.json available; v0 (flat task list) output only")

		return nil
	}

	manifest, err := buildanalyzer.ReadManifest(path)
	if err != nil {
		fmt.Fprintf(stderr, "analyze: manifest.json read failed (%v); falling back to v0 flat task list\n", err)

		return nil
	}

	buildanalyzer.JoinManifest(report, manifest)

	return nil
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

// resolveAnalyzeManifestPath picks the manifest.json path. Explicit flag
// wins; otherwise auto-discovers via --derived-data. Empty return without an
// error means "no manifest to join against" — the caller soft-falls back to
// v0 output. filepath.Clean is called on the explicit path so relative inputs
// still match across getwd shifts.
func resolveAnalyzeManifestPath(explicit, derivedData string) (string, error) {
	if explicit != "" {
		return filepath.Clean(explicit), nil
	}

	if derivedData == "" {
		return "", nil
	}

	path, err := buildanalyzer.LocateManifest(derivedData)
	if err != nil {
		return "", fmt.Errorf("locate manifest.json under %s: %w", derivedData, err)
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
		if _, err := fmt.Fprintf(out, "%s  %10s  exit=%d  %s\n",
			task.StartedAt.Format("15:04:05.000"),
			task.Duration,
			task.ExitCode,
			truncate(task.Key, 160),
		); err != nil {
			return fmt.Errorf("write pretty task line: %w", err)
		}

		for _, errLine := range task.Errors {
			if _, err := fmt.Fprintf(out, "    ! %s\n", errLine); err != nil {
				return fmt.Errorf("write pretty error line: %w", err)
			}
		}
	}

	return nil
}

func formatTimestamp(s string) string {
	if s == "" {
		return "-"
	}

	return s
}
