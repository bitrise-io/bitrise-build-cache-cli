package browse

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	browsepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/browse"
)

//nolint:gochecknoglobals
var browseParams struct {
	workspaceID string
	printOnly   bool
	jsonOutput  bool
}

//nolint:gochecknoglobals
var browseCmd = &cobra.Command{
	Use:   "browse [invocation-id]",
	Short: "Open the Bitrise Build Cache dashboard for the configured workspace",
	Long: `browse opens the user's default browser at the Bitrise Build Cache dashboard, ` +
		`pre-filtered to the configured workspace (BITRISE_BUILD_CACHE_WORKSPACE_ID or --workspace; ` +
		`falls back to the workspace stored by ` + "`auth set`" + ` / ` + "`activate`" + `) and ` +
		`to ` + "`ci_provider=unknown`" + ` (closest "my local invocations" filter until the BE adds a username field). ` +
		`Pass an optional invocation ID positional argument to deep-link to a specific invocation page. ` +
		`Use ` + "`--print`" + ` to skip the launcher and only emit the URL — useful in headless / CI sessions.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		params := browsepkg.Params{
			WorkspaceID: browseParams.workspaceID,
			Envs:        utils.AllEnvs(),
			PrintOnly:   browseParams.printOnly || browseParams.jsonOutput,
		}
		if len(args) == 1 {
			params.InvocationID = args[0]
		}

		browserLogger := logger
		if browseParams.jsonOutput {
			browserLogger = nil
		}

		b := &browsepkg.Browser{Logger: browserLogger}
		res, err := b.Open(cmd.Context(), params)
		if err != nil {
			if errors.Is(err, browsepkg.ErrWorkspaceNotConfigured) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("browse: %w", err)
		}

		if browseParams.jsonOutput {
			enc := json.NewEncoder(cmd.OutOrStdout())
			if err := enc.Encode(res); err != nil {
				return fmt.Errorf("encode browse result: %w", err)
			}
		}

		return nil
	},
}

func init() {
	browseCmd.Flags().StringVar(
		&browseParams.workspaceID,
		"workspace",
		"",
		"Workspace slug. Falls back to BITRISE_BUILD_CACHE_WORKSPACE_ID, then to the workspace stored in the OS keychain / multiplatform-analytics config (whichever `auth set` or `activate` wrote).",
	)
	browseCmd.Flags().BoolVar(
		&browseParams.printOnly,
		"print",
		false,
		"Skip launching the default browser. The dashboard URL is always logged at Info level; this flag only suppresses the auto-open step. Useful in headless or CI sessions.",
	)
	browseCmd.Flags().BoolVar(
		&browseParams.jsonOutput,
		"json",
		false,
		"Emit `{url, workspace_id, invocation_id}` as a single JSON object on stdout and skip both the human log line and the auto-open. Useful for downstream automation.",
	)

	common.RootCmd.AddCommand(browseCmd)
}
