package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/status"
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
	Long:          "Reports gradle / xcode / cpp / react-native activation status. Intended for step integrations that need to decide whether to engage cache wrapping.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runStatus(cmd.OutOrStdout(), cmd.ErrOrStderr(), status.NewChecker(status.CheckerParams{}))
	},
}

func init() {
	statusCmd.Flags().BoolVar(&statusJSONOutput, "json", false, "Emit machine-readable JSON instead of a text table")
	statusCmd.Flags().StringVar(&statusFeature, "feature", "", "Query a single feature: gradle, xcode, cpp, react-native")
	statusCmd.Flags().BoolVar(&statusQuiet, "quiet", false, "Suppress stdout; only meaningful with --feature (exit 0=enabled, 1=disabled, 2=unknown feature or misuse). Takes precedence over --json.")

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
	auth := currentAuthStatus()
	if statusJSONOutput {
		return writeJSON(out, statusOutput{
			Gradle:      s.Gradle,
			Xcode:       s.Xcode,
			Cpp:         s.Cpp,
			ReactNative: s.ReactNative,
			Auth:        auth,
		})
	}

	if err := writeTable(out, s); err != nil {
		return err
	}

	return writeAuthLine(out, auth)
}

// runStatusFeature handles --feature queries. Precedence when combined:
// --quiet wins over --json (silent exit-code semantics beat machine output).
func runStatusFeature(out, errOut io.Writer, checker *status.Checker) error {
	enabled, err := checker.IsEnabled(statusFeature)
	if err != nil {
		if errors.Is(err, status.ErrUnknownFeature) {
			fmt.Fprintf(errOut, "error: unknown feature %q (expected: gradle, xcode, cpp, react-native)\n", statusFeature)

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

// authStatusInfo summarizes the credential the build cache will use, for status
// output. Read-only; never includes token material.
type authStatusInfo struct {
	Configured  bool   `json:"configured"`
	Source      string `json:"source"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	TokenExpiry string `json:"token_expiry,omitempty"`
	Expired     bool   `json:"expired,omitempty"`
	Error       string `json:"error,omitempty"`
}

// statusOutput is the --json shape: feature flags plus auth.
type statusOutput struct {
	Gradle      bool           `json:"gradle"`
	Xcode       bool           `json:"xcode"`
	Cpp         bool           `json:"cpp"`
	ReactNative bool           `json:"reactNative"`
	Auth        authStatusInfo `json:"auth"`
}

// currentAuthStatus reports the credential commands would use, via config
// common's resolution + shared AuthDescription. Never refreshes or writes.
func currentAuthStatus() authStatusInfo {
	cfg, source, err := configcommon.ResolveAuthConfig(utils.AllEnvs())
	switch {
	case errors.Is(err, configcommon.ErrAuthTokenNotProvided), errors.Is(err, configcommon.ErrWorkspaceIDNotProvided):
		return authStatusInfo{Source: "none"}
	case err != nil:
		// A real resolution failure (e.g. a malformed service JWT) — surface it
		// rather than mislabel it "not configured".
		return authStatusInfo{Source: "error", Error: err.Error()}
	case cfg.AuthToken == "":
		return authStatusInfo{Source: "none"}
	}

	d := configcommon.DescribeResolved(cfg, source)
	info := authStatusInfo{
		Configured:  true,
		Source:      d.Label(),
		WorkspaceID: d.WorkspaceID,
	}
	if !d.PATExpiry.IsZero() {
		info.TokenExpiry = d.PATExpiry.Format(time.RFC3339)
		info.Expired = d.Expired()
	}

	return info
}

// writeAuthLine appends a one-line auth summary to the text status output.
func writeAuthLine(out io.Writer, a authStatusInfo) error {
	line := "Auth: "
	switch {
	case a.Error != "":
		line += "could not resolve credentials: " + a.Error
	case !a.Configured:
		line += "not configured (run 'bitrise-build-cache auth login' or 'bitrise-build-cache activate --interactive', or set BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID)"
	default:
		line += a.Source
		if a.WorkspaceID != "" {
			line += fmt.Sprintf(" (workspace %s)", a.WorkspaceID)
		}
		switch {
		case a.TokenExpiry == "":
		case a.Expired:
			line += ", token expired — refreshes on next use"
		default:
			line += ", token valid until " + a.TokenExpiry
		}
	}
	if _, err := fmt.Fprintln(out, "\n"+line); err != nil {
		return fmt.Errorf("write auth status: %w", err)
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
