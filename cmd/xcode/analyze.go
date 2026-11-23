package xcode

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

const (
	outputJSON   = "json"
	outputPretty = "pretty"
)

//nolint:gochecknoglobals
var (
	analyzeFlagActivityLog string
	analyzeFlagDerivedData string
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
  v0 (this subcommand) — .xcactivitylog → flat task list, JSON or pretty.
  v1 — join against the manifest-*.xcbuild graph.
  v2 — annotate execution reason via build.db signature diff.
  v3 — rich clang diagnostics via .dia parse.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runAnalyze(cmd.OutOrStdout())
	},
}

func init() {
	xcodeCommand.AddCommand(analyzeCmd)

	analyzeCmd.Flags().StringVar(&analyzeFlagActivityLog, "xcactivitylog", "", "Path to an .xcactivitylog file. Overrides --derived-data when both are set.")
	analyzeCmd.Flags().StringVar(&analyzeFlagDerivedData, "derived-data", "", "DerivedData path; the newest Logs/Build/*.xcactivitylog underneath is picked.")
	analyzeCmd.Flags().StringVar(&analyzeFlagOutput, "output", outputJSON, "Output format: `json` (default) or `pretty`.")
}

func runAnalyze(out io.Writer) error {
	path, err := resolveAnalyzeLogPath(analyzeFlagActivityLog, analyzeFlagDerivedData)
	if err != nil {
		return err
	}

	report, err := buildanalyzer.ReadReport(path)
	if err != nil {
		return fmt.Errorf("analyze %s: %w", path, err)
	}

	switch analyzeFlagOutput {
	case outputPretty:
		return renderPretty(out, report)
	case outputJSON, "":
		return renderJSON(out, report)
	default:
		return fmt.Errorf("unknown --output %q (want json|pretty)", analyzeFlagOutput)
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
