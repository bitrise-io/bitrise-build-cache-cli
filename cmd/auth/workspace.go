package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/bitriseapi"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type workspaceOutput struct {
	WorkspaceID string `json:"workspace_id"`
	Source      string `json:"source"`
}

// Flags live on the command rather than in package globals so each invocation —
// including a test's — starts from a clean slate.
func newAuthWorkspaceCmd() *cobra.Command {
	var (
		listOut  bool
		setValue string
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Show, list or select the workspace the build cache uses",
		Long: `Without a flag: prints the workspace the CLI would use, resolved through the
same precedence chain as the rest of the CLI.

--list shows every workspace the stored credential can access, and --set pins one
of them without a new browser sign-in. Together they are the two halves of the
workspace picker ` + "`auth login`" + ` shows on a terminal, split so a session that has no
terminal — an agent driving the CLI on a remote host, a script — can sign in with
` + "`auth login --no-workspace`" + ` and select afterwards.

--json makes both the printed workspace and the listing machine-readable.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			envs := utils.AllEnvs()
			logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

			switch {
			case listOut && cmd.Flags().Changed("set"):
				return errors.New("--list and --set are mutually exclusive")
			case listOut:
				return listWorkspaces(cmd, envs, jsonOut)
			case cmd.Flags().Changed("set"):
				return setWorkspace(cmd.Context(), logger, envs, setValue)
			}

			return printWorkspace(cmd, envs, jsonOut)
		},
	}

	cmd.Flags().BoolVar(&listOut, "list", false, "List every workspace the stored credential can access, instead of printing the selected one.")
	cmd.Flags().StringVar(&setValue, "set", "", "Pin this workspace slug into the store that holds your credentials, leaving the token untouched.")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Machine-readable output, for --list and for the printed workspace.")

	return cmd
}

func printWorkspace(cmd *cobra.Command, envs map[string]string, jsonOut bool) error {
	cred, origin, err := live.Default(nil).ResolveNoRefresh(envs)
	// Not-selected-yet is what this command exists to report, not a failure.
	if err != nil && !errors.Is(err, authpkg.ErrWorkspaceNotSelected) {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err.Error())

		return err //nolint:wrapcheck // already user-facing
	}

	out := cmd.OutOrStdout()
	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(workspaceOutput{WorkspaceID: cred.WorkspaceID, Source: origin.ShortLabel()}); encErr != nil {
			return fmt.Errorf("encode workspace json: %w", encErr)
		}

		return nil
	}

	if _, wErr := fmt.Fprintln(out, cred.WorkspaceID); wErr != nil {
		return fmt.Errorf("write workspace: %w", wErr)
	}

	return nil
}

func listWorkspaces(cmd *cobra.Command, envs map[string]string, jsonOut bool) error {
	ctx := cmd.Context()

	cred, _, err := live.Default(nil).ResolveTokenOnly(ctx, envs)
	if err != nil {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err.Error())

		return err //nolint:wrapcheck // already user-facing
	}

	workspaces, err := bitriseapi.ListWorkspaces(ctx, bitriseapi.ResolveAPIBaseURL(envs), cred.Token)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	out := cmd.OutOrStdout()
	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(workspaces); encErr != nil {
			return fmt.Errorf("encode workspaces json: %w", encErr)
		}

		return nil
	}

	for _, ws := range workspaces {
		if _, wErr := fmt.Fprintf(out, "%s\t%s\n", ws.Slug, ws.Name); wErr != nil {
			return fmt.Errorf("write workspaces: %w", wErr)
		}
	}

	return nil
}

func setWorkspace(ctx context.Context, logger log.Logger, envs map[string]string, slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return errors.New("--set needs a workspace slug (list them with `auth workspace --list`)")
	}

	warnUnknownWorkspace(ctx, logger, envs, slug)

	origin, err := store.SetWorkspaceID(configcommon.DetectCIProvider(envs) != "", slug)
	if err != nil {
		return err //nolint:wrapcheck // already user-facing
	}

	logger.TInfof("✅ Using workspace %q for the build cache (stored in the %s).", slug, origin.Label())
	if shadow := shadowingWorkspaceEnv(envs); shadow != "" {
		logger.Warnf("%s is set and takes precedence — unset it to use the workspace just stored.", shadow)
	}

	return nil
}

// warnUnknownWorkspace flags a slug the credential can't see. It only warns: the
// listing needs the network, and an offline machine must still be able to pin a
// workspace the user knows is right.
func warnUnknownWorkspace(ctx context.Context, logger log.Logger, envs map[string]string, slug string) {
	cred, _, err := live.Default(nil).ResolveTokenOnly(ctx, envs)
	if err != nil {
		return
	}

	workspaces, err := bitriseapi.ListWorkspaces(ctx, bitriseapi.ResolveAPIBaseURL(envs), cred.Token)
	if err != nil {
		return
	}

	for _, ws := range workspaces {
		if ws.Slug == slug {
			return
		}
	}

	logger.Warnf("%q is not among the workspaces this login can access — storing it anyway, but the build cache will reject it.", slug)
	logger.Warnf("Run `bitrise-build-cache auth workspace --list` to see the ones it can.")
}

func shadowingWorkspaceEnv(envs map[string]string) string {
	switch {
	case envs[authpkg.EnvAuthToken] != "" && envs[authpkg.EnvWorkspaceID] != "":
		return authpkg.EnvWorkspaceID
	case envs[authpkg.EnvJWT] != "":
		return authpkg.EnvJWT
	}

	return ""
}
