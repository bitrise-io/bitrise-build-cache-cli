package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/invocations"
)

// ACI-4901: user-facing cobra wrappers for the `internal/invocations`
// package. Authentication is the bitrise-website Personal Access Token
// (NOT the build-cache workspace token); resolved from a flag / env / the
// `~/.bitrise/cache/ccache/config.json` fallback so the same PAT the
// hackathon stack already holds works without re-prompting the user.

//nolint:gochecknoglobals
var (
	invocationsBaseURL       string
	invocationsToken         string
	invocationsWorkspaceSlug string
	invocationsJSONOutput    bool
)

//nolint:gochecknoglobals
var (
	// stats flags
	invocationsStatsSource    string
	invocationsStatsDirectURL string
	invocationsStatsMaxItems  int
)

//nolint:gochecknoglobals
var (
	// list flags
	invocationsListTool         string
	invocationsListStatus       string
	invocationsListProject      string
	invocationsListBuild        string
	invocationsListWorkflow     string
	invocationsListCIProvider   string
	invocationsListCommand      string
	invocationsListBefore       string
	invocationsListAfter        string
	invocationsListOrderBy      string
	invocationsListOrderDir     string
	invocationsListPage         int
	invocationsListItemsPerPage int

	// get / tasks flags
	invocationsGetTool string
)

//nolint:gochecknoglobals
var invocationsCmd = &cobra.Command{
	Use:           "invocations",
	Short:         "Query the Bitrise Build Cache invocations API",
	Long:          "Subcommands wrap GET /build-cache/:workspace/invocations on bitrise-website. Returns JSON suited for downstream tooling (e.g. the desktop app).",
	SilenceUsage:  true,
	SilenceErrors: true,
}

//nolint:gochecknoglobals
var invocationsListCmd = &cobra.Command{
	Use:           "list",
	Short:         "List invocations matching the given filters",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newInvocationsClient(cmd)
		if err != nil {
			return err
		}

		filter, err := buildInvocationsListFilter()
		if err != nil {
			return err
		}

		resp, err := client.List(filter)
		if err != nil {
			return fmt.Errorf("list invocations: %w", err)
		}

		return WriteJSON(cmd.OutOrStdout(), resp)
	},
}

//nolint:gochecknoglobals
var invocationsGetCmd = &cobra.Command{
	Use:           "get <invocation-id>",
	Short:         "Get an invocation by ID",
	Long:          "Returns the BuildToolInvocationInfoPresenter JSON for a single invocation.",
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newInvocationsClient(cmd)
		if err != nil {
			return err
		}

		raw, err := client.Get(invocationsGetTool, args[0])
		if err != nil {
			return fmt.Errorf("get invocation: %w", err)
		}

		// `Get` already returns json.RawMessage; pretty-print verbatim so
		// callers see the full presenter shape, not a re-marshalling that
		// might drop unknown fields.
		if _, err := cmd.OutOrStdout().Write(raw); err != nil {
			return fmt.Errorf("write response: %w", err)
		}

		if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
			return fmt.Errorf("write response newline: %w", err)
		}

		return nil
	},
}

//nolint:gochecknoglobals
var invocationsStatsCmd = &cobra.Command{
	Use:           "stats",
	Short:         "Aggregate count + hit rate P50 + estimated time saved",
	Long:          "Two sources:\n  --source list   (default) — page through GET /build-cache/:ws/invocations.json on bitrise-website and aggregate client-side. Works against the public website API.\n  --source direct           — call GET /internal/invocations/stats on xcode-analytics-service. Faster (single roundtrip) but requires direct service access; xcode-only.\n\nFilter flags match `invocations list` (tool, project, build, workflow, ci-provider, status, command, before, after).",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		filter, err := buildInvocationsListFilter()
		if err != nil {
			return err
		}

		switch invocationsStatsSource {
		case "direct":
			return runInvocationsStatsDirect(cmd, filter)
		case "list", "":
			return runInvocationsStatsFromList(cmd, filter)
		default:
			return fmt.Errorf("unknown --source %q (use list or direct)", invocationsStatsSource)
		}
	},
}

func runInvocationsStatsFromList(cmd *cobra.Command, filter invocations.ListFilter) error {
	client, err := newInvocationsClient(cmd)
	if err != nil {
		return err
	}

	stats, err := invocations.AggregateFromList(client, filter, invocationsStatsMaxItems)
	if err != nil {
		return fmt.Errorf("aggregate: %w", err)
	}

	return WriteJSON(cmd.OutOrStdout(), stats)
}

func runInvocationsStatsDirect(cmd *cobra.Command, filter invocations.ListFilter) error {
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))

	token, workspace, err := resolveInvocationsAuth()
	if err != nil {
		return err
	}

	directClient := invocations.NewDirectClient(invocationsStatsDirectURL, token, workspace, logger)

	stats, err := directClient.Stats(invocations.DirectListFilter{
		AppSlug:       filter.ProjectSlug,
		BuildSlug:     filter.BuildSlug,
		WorkflowName:  filter.Workflow,
		Command:       filter.Command,
		ProviderID:    filter.CIProvider,
		RepositoryURL: filter.RepositoryURL,
		Before:        filter.Before,
		After:         filter.After,
	})
	if err != nil {
		return fmt.Errorf("direct stats: %w", err)
	}

	return WriteJSON(cmd.OutOrStdout(), stats)
}

//nolint:gochecknoglobals
var invocationsTasksCmd = &cobra.Command{
	Use:           "tasks <invocation-id>",
	Short:         "List Gradle tasks for an invocation",
	Long:          "Returns the per-task breakdown for a Gradle invocation. Bazel uses `targets`; not implemented here yet.",
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newInvocationsClient(cmd)
		if err != nil {
			return err
		}

		resp, err := client.GetGradleTasks(args[0])
		if err != nil {
			return fmt.Errorf("get gradle tasks: %w", err)
		}

		return WriteJSON(cmd.OutOrStdout(), resp)
	},
}

func newInvocationsClient(cmd *cobra.Command) (*invocations.Client, error) {
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))

	token, workspace, err := resolveInvocationsAuth()
	if err != nil {
		return nil, err
	}

	logger.Debugf("invocations API: baseURL=%s workspace=%s", invocationsBaseURL, workspace)
	_ = cmd

	return invocations.NewClient(invocationsBaseURL, token, workspace, logger), nil
}

// resolveInvocationsAuth picks the workspace + PAT from (in order): flags,
// env (`BITRISE_TOKEN`, `BITRISE_WORKSPACE_ID`), then the local ccache
// config at `~/.bitrise/cache/ccache/config.json` — same source the
// hackathon snapshot tooling reads.
func resolveInvocationsAuth() (string, string, error) {
	token := invocationsToken
	if token == "" {
		token = os.Getenv("BITRISE_TOKEN")
	}

	workspace := invocationsWorkspaceSlug
	if workspace == "" {
		workspace = os.Getenv("BITRISE_WORKSPACE_ID")
	}

	token, workspace = fillFromConfigIfMissing(token, workspace)

	if token == "" {
		return "", "", fmt.Errorf("missing PAT — set --token, BITRISE_TOKEN, or AuthToken in ~/.bitrise/cache/ccache/config.json")
	}

	if workspace == "" {
		return "", "", fmt.Errorf("missing workspace — set --workspace, BITRISE_WORKSPACE_ID, or WorkspaceID in ~/.bitrise/cache/ccache/config.json")
	}

	return token, workspace, nil
}

func fillFromConfigIfMissing(token, workspace string) (string, string) {
	if token != "" && workspace != "" {
		return token, workspace
	}

	fromConfig, err := readCcacheConfigAuth()
	if err != nil {
		return token, workspace
	}

	if token == "" {
		token = fromConfig.token
	}
	if workspace == "" {
		workspace = fromConfig.workspace
	}

	return token, workspace
}

func buildInvocationsListFilter() (invocations.ListFilter, error) {
	filter := invocations.ListFilter{
		Tool:           invocationsListTool,
		Page:           invocationsListPage,
		ItemsPerPage:   invocationsListItemsPerPage,
		OrderBy:        invocationsListOrderBy,
		OrderDirection: invocationsListOrderDir,
		ProjectSlug:    invocationsListProject,
		BuildSlug:      invocationsListBuild,
		Workflow:       invocationsListWorkflow,
		CIProvider:     invocationsListCIProvider,
		Status:         invocationsListStatus,
		Command:        invocationsListCommand,
	}

	if invocationsListBefore != "" {
		t, err := time.Parse(time.RFC3339, invocationsListBefore)
		if err != nil {
			return filter, fmt.Errorf("parse --before (RFC 3339): %w", err)
		}
		filter.Before = t
	}

	if invocationsListAfter != "" {
		t, err := time.Parse(time.RFC3339, invocationsListAfter)
		if err != nil {
			return filter, fmt.Errorf("parse --after (RFC 3339): %w", err)
		}
		filter.After = t
	}

	return filter, nil
}

func init() { //nolint:gochecknoinits
	RootCmd.AddCommand(invocationsCmd)

	// Shared flags on the parent — propagate to children via cobra's
	// PersistentFlags lookup.
	invocationsCmd.PersistentFlags().StringVar(&invocationsBaseURL, "base-url", invocations.DefaultBaseURL, "Bitrise API base URL")
	invocationsCmd.PersistentFlags().StringVar(&invocationsToken, "token", "", "Bitrise Personal Access Token (default: $BITRISE_TOKEN, then ~/.bitrise/cache/ccache/config.json)")
	invocationsCmd.PersistentFlags().StringVar(&invocationsWorkspaceSlug, "workspace", "", "Bitrise workspace / org slug (default: $BITRISE_WORKSPACE_ID, then ~/.bitrise/cache/ccache/config.json)")
	invocationsCmd.PersistentFlags().BoolVar(&invocationsJSONOutput, "json", true, "Emit machine-readable JSON to stdout (currently always-on)")

	invocationsCmd.AddCommand(invocationsListCmd)
	invocationsListCmd.Flags().StringVar(&invocationsListTool, "tool", invocations.BuildToolGradle, "Build tool: gradle / bazel / xcode / react-native")
	invocationsListCmd.Flags().StringVar(&invocationsListStatus, "status", "", "Filter: success / failed")
	invocationsListCmd.Flags().StringVar(&invocationsListProject, "project", "", "Bitrise app slug filter")
	invocationsListCmd.Flags().StringVar(&invocationsListBuild, "build", "", "Bitrise build slug filter")
	invocationsListCmd.Flags().StringVar(&invocationsListWorkflow, "workflow", "", "Workflow name filter")
	invocationsListCmd.Flags().StringVar(&invocationsListCIProvider, "ci-provider", "", "CI provider filter (e.g. bitrise / github / gitlab; pass 'unknown' for local)")
	invocationsListCmd.Flags().StringVar(&invocationsListCommand, "command", "", "Command filter (e.g. assemble / build)")
	invocationsListCmd.Flags().StringVar(&invocationsListBefore, "before", "", "Upper time bound, RFC 3339")
	invocationsListCmd.Flags().StringVar(&invocationsListAfter, "after", "", "Lower time bound, RFC 3339")
	invocationsListCmd.Flags().StringVar(&invocationsListOrderBy, "order-by", "", "started_at / cache_hit / duration")
	invocationsListCmd.Flags().StringVar(&invocationsListOrderDir, "order-direction", "", "ascending / descending")
	invocationsListCmd.Flags().IntVar(&invocationsListPage, "page", 0, "Page number, 1-based")
	invocationsListCmd.Flags().IntVar(&invocationsListItemsPerPage, "items-per-page", 0, "Items per page (1–100)")

	invocationsCmd.AddCommand(invocationsGetCmd)
	invocationsGetCmd.Flags().StringVar(&invocationsGetTool, "tool", invocations.BuildToolGradle, "Build tool of the invocation: gradle / bazel / xcode / react-native / ccache")

	invocationsCmd.AddCommand(invocationsTasksCmd)

	invocationsCmd.AddCommand(invocationsStatsCmd)
	invocationsStatsCmd.Flags().StringVar(&invocationsStatsSource, "source", "list", "Aggregation source: list (paginated, via bitrise-website) or direct (xcode-analytics-service)")
	invocationsStatsCmd.Flags().StringVar(&invocationsStatsDirectURL, "direct-url", invocations.XcodeServiceDefaultBaseURL, "Base URL for the direct source (use http://localhost:3000 for the local stack)")
	invocationsStatsCmd.Flags().IntVar(&invocationsStatsMaxItems, "max-items", 1000, "Cap for client-side aggregation (--source list only)")
	invocationsStatsCmd.Flags().StringVar(&invocationsListTool, "tool", invocations.BuildToolGradle, "Build tool filter")
	invocationsStatsCmd.Flags().StringVar(&invocationsListStatus, "status", "", "Filter: success / failed")
	invocationsStatsCmd.Flags().StringVar(&invocationsListProject, "project", "", "Bitrise app slug filter")
	invocationsStatsCmd.Flags().StringVar(&invocationsListBuild, "build", "", "Bitrise build slug filter")
	invocationsStatsCmd.Flags().StringVar(&invocationsListWorkflow, "workflow", "", "Workflow name filter")
	invocationsStatsCmd.Flags().StringVar(&invocationsListCIProvider, "ci-provider", "", "CI provider filter (use 'unknown' for local-only)")
	invocationsStatsCmd.Flags().StringVar(&invocationsListCommand, "command", "", "Command filter")
	invocationsStatsCmd.Flags().StringVar(&invocationsListBefore, "before", "", "Upper time bound, RFC 3339")
	invocationsStatsCmd.Flags().StringVar(&invocationsListAfter, "after", "", "Lower time bound, RFC 3339")
}

// ccacheConfigAuth captures the workspace + PAT we look up from the
// ccache config as a last-resort source.
type ccacheConfigAuth struct {
	token     string
	workspace string
}

func readCcacheConfigAuth() (ccacheConfigAuth, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ccacheConfigAuth{}, fmt.Errorf("user home dir: %w", err)
	}

	path := filepath.Join(homeDir, ".bitrise", "cache", "ccache", "config.json")

	raw, err := os.ReadFile(path)
	if err != nil {
		return ccacheConfigAuth{}, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg struct {
		AuthConfig struct {
			AuthToken   string `json:"AuthToken"`
			WorkspaceID string `json:"WorkspaceID"`
		} `json:"authConfig"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ccacheConfigAuth{}, fmt.Errorf("decode %s: %w", path, err)
	}

	return ccacheConfigAuth{
		token:     cfg.AuthConfig.AuthToken,
		workspace: cfg.AuthConfig.WorkspaceID,
	}, nil
}
