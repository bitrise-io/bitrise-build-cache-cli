package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/status"
)

//nolint:gochecknoglobals
var (
	statusJSONOutput bool
	statusFeature    string
	statusQuiet      bool
)

//nolint:gochecknoglobals
var statusCmd = &cobra.Command{
	Use:           "status",
	Short:         "Show which Bitrise Build Cache features are enabled on this machine",
	Long:          "Reports gradle / xcode / cpp / react-native / bazel activation status. Intended for step integrations that need to decide whether to engage cache wrapping.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runStatus(cmd.OutOrStdout(), cmd.ErrOrStderr(), status.NewChecker(status.CheckerParams{}))
	},
}

func init() {
	statusCmd.Flags().BoolVar(&statusJSONOutput, "json", false, "Emit machine-readable JSON instead of a text table")
	statusCmd.Flags().StringVar(&statusFeature, "feature", "", "Query a single feature: gradle, xcode, cpp, react-native, bazel")
	statusCmd.Flags().BoolVar(&statusQuiet, "quiet", false, "Suppress stdout; only meaningful with --feature (exit 0=enabled, 1=disabled)")

	RootCmd.AddCommand(statusCmd)
}

// statusExitError lets us signal a non-zero exit code without letting cobra
// print the error (we want silent-exit semantics for `--quiet`).
type statusExitError struct{ code int }

func (e *statusExitError) Error() string { return fmt.Sprintf("status exit code %d", e.code) }

// HandleStatusExit converts a statusExitError returned by Execute into an
// os.Exit code. Other errors fall through to the caller.
func HandleStatusExit(err error) (int, bool) {
	var se *statusExitError
	if errors.As(err, &se) {
		return se.code, true
	}

	return 0, false
}

func runStatus(out, errOut io.Writer, checker *status.Checker) error {
	if statusFeature != "" {
		return runStatusFeature(out, errOut, checker)
	}

	if statusQuiet {
		fmt.Fprintln(errOut, "error: --quiet requires --feature")

		return &statusExitError{code: 2}
	}

	s := checker.Status()
	if statusJSONOutput {
		return writeJSON(out, s)
	}

	return writeTable(out, s)
}

func runStatusFeature(out, errOut io.Writer, checker *status.Checker) error {
	enabled, err := checker.IsEnabled(statusFeature)
	if err != nil {
		if errors.Is(err, status.ErrUnknownFeature) {
			fmt.Fprintf(errOut, "error: unknown feature %q (expected: gradle, xcode, cpp, react-native, bazel)\n", statusFeature)

			return &statusExitError{code: 2}
		}

		return fmt.Errorf("query status: %w", err)
	}

	if statusQuiet {
		if enabled {
			return nil
		}

		return &statusExitError{code: 1}
	}

	if statusJSONOutput {
		payload := map[string]bool{jsonKey(statusFeature): enabled}

		return writeJSON(out, payload)
	}

	if enabled {
		fmt.Fprintln(out, "enabled")
	} else {
		fmt.Fprintln(out, "disabled")
	}

	return nil
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode status JSON: %w", err)
	}

	return nil
}

func writeTable(out io.Writer, s status.Status) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, row := range [...]struct {
		name    string
		enabled bool
	}{
		{"gradle", s.Gradle},
		{"xcode", s.Xcode},
		{"cpp", s.Cpp},
		{"react-native", s.ReactNative},
		{"bazel", s.Bazel},
	} {
		if _, err := fmt.Fprintf(tw, "%s\t%s\n", row.name, statusLabel(row.enabled)); err != nil {
			return fmt.Errorf("write status row: %w", err)
		}
	}

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush status table: %w", err)
	}

	return nil
}

func statusLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}

	return "disabled"
}

// jsonKey maps CLI feature names (dash-case) to the JSON field name in
// status.Status (camelCase), so `--feature=react-native --json` matches the
// unfiltered `--json` output shape.
func jsonKey(feature string) string {
	switch feature {
	case status.FeatureReactNative:
		return "reactNative"
	default:
		return feature
	}
}
