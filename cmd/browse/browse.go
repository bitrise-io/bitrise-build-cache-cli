// Package browse exposes the `bitrise-build-cache browse` cobra subcommand.
// Thin wrapper that maps flags into pkg/browse and prints the dashboard URL.
package browse

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
	browsepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/browse"
)

//nolint:gochecknoglobals
var browseParams struct {
	workspaceID string
	printOnly   bool
}

//nolint:gochecknoglobals
var browseCmd = &cobra.Command{
	Use:   "browse [invocation-id]",
	Short: "Open the Bitrise Build Cache dashboard for the configured workspace",
	Long: `browse opens the user's default browser at the Bitrise Build Cache dashboard, ` +
		`pre-filtered to the configured workspace (BITRISE_BUILD_CACHE_WORKSPACE_ID) and ` + "`source=local`" + ` invocations. ` +
		`Pass an optional invocation ID positional argument to deep-link to a specific invocation page. ` +
		`Use ` + "`--print`" + ` to skip the launcher and only emit the URL — useful in headless / CI sessions.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		params := browsepkg.Params{
			WorkspaceID: browseParams.workspaceID,
			Envs:        utils.AllEnvs(),
			PrintOnly:   browseParams.printOnly,
		}
		if len(args) == 1 {
			params.InvocationID = args[0]
		}

		b := &browsepkg.Browser{Logger: logger}
		if _, err := b.Open(cmd.Context(), params); err != nil {
			if errors.Is(err, browsepkg.ErrWorkspaceNotConfigured) {
				return err //nolint:wrapcheck // sentinel
			}

			return fmt.Errorf("browse: %w", err)
		}

		return nil
	},
}

func init() {
	browseCmd.Flags().StringVar(
		&browseParams.workspaceID,
		"workspace",
		"",
		"Workspace slug. Falls back to BITRISE_BUILD_CACHE_WORKSPACE_ID env var when empty.",
	)
	browseCmd.Flags().BoolVar(
		&browseParams.printOnly,
		"print",
		false,
		"Skip launching the default browser. The dashboard URL is always logged at Info level; this flag only suppresses the auto-open step. Useful in headless or CI sessions.",
	)

	common.RootCmd.AddCommand(browseCmd)
}
