package common

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/bitriseapi"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

var loginWorkspace string //nolint:gochecknoglobals

// LoginCmd signs the user in via the browser (OAuth) and stores a managed,
// auto-refreshing credential for local build-cache use.
var LoginCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "login",
	Short: "Sign in to Bitrise to use the build cache locally (browser OAuth)",
	Long: `Sign in to Bitrise via the browser and store a managed, auto-refreshing
credential for local build-cache use — so you don't have to create a Personal
Access Token and set BITRISE_BUILD_CACHE_AUTH_TOKEN / BITRISE_BUILD_CACHE_WORKSPACE_ID
by hand.

Nothing changes on Bitrise CI (the build still uses the auto-provided service
token), and a manually-set BITRISE_BUILD_CACHE_AUTH_TOKEN still takes precedence.

This needs a browser on the same machine as the CLI (the sign-in is handed back
over a loopback address); it can't complete on a remote/headless host — there,
keep using BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runLogin(cmd)
	},
}

// LogoutCmd removes the stored OAuth credential.
var LogoutCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "logout",
	Short: "Remove the stored Bitrise build-cache login",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := oauth.Clear(); err != nil {
			return fmt.Errorf("clear stored login: %w", err)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Signed out.")

		return nil
	},
}

func init() { //nolint:gochecknoinits
	LoginCmd.Flags().StringVar(&loginWorkspace, "workspace", "", "workspace (organization) slug to use; skips the interactive picker")
	RootCmd.AddCommand(LoginCmd)
	RootCmd.AddCommand(LogoutCmd)
}

func runLogin(cmd *cobra.Command) error {
	ctx := cmd.Context()
	envs := utils.AllEnvs()
	stderr := cmd.ErrOrStderr()

	if loginWorkspace == "" && !isInteractiveStdin() {
		return fmt.Errorf("not an interactive terminal: pass --workspace <slug> to sign in non-interactively")
	}

	creds, err := oauth.NewConfigFromEnv(envs).Login(ctx, oauth.OpenBrowser, stderr)
	if err != nil {
		return fmt.Errorf("sign in: %w", err)
	}

	workspace := loginWorkspace
	if workspace == "" {
		workspace, err = pickWorkspace(ctx, cmd, envs, creds.PAT)
		if err != nil {
			return err
		}
	}
	creds.WorkspaceID = workspace

	if err := oauth.Save(creds); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	fmt.Fprintf(stderr, "\n✓ Signed in. Using workspace %q for the build cache.\n", workspace)

	if shadow := shadowingAuthEnv(); shadow != "" {
		fmt.Fprintf(stderr, "\n⚠ %s is set and takes precedence over the login just saved.\n", shadow)
		fmt.Fprintf(stderr, "  Build-cache commands will use it, not this login — unset it to use the stored login.\n")
	}

	return nil
}

// shadowingAuthEnv returns the name of an environment credential that takes
// precedence over the stored OAuth login, or "" if none is set. These are
// resolved before the stored login (see hydrateStoredAuth), so when one is set
// the login has no effect on later commands — a common, confusing failure mode,
// hence the warning at login time. BITRISE_BUILD_CACHE_AUTH_TOKEN is checked
// first because it's the one a local user is likely to have set by hand.
func shadowingAuthEnv() string {
	if os.Getenv("BITRISE_BUILD_CACHE_AUTH_TOKEN") != "" {
		return "BITRISE_BUILD_CACHE_AUTH_TOKEN"
	}
	if os.Getenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN") != "" {
		return "BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN"
	}

	return ""
}

// pickWorkspace lists the workspaces the fresh PAT can access and lets the user
// choose one (auto-selecting when there's exactly one).
func pickWorkspace(ctx context.Context, cmd *cobra.Command, envs map[string]string, pat string) (string, error) {
	workspaces, err := bitriseapi.ListWorkspaces(ctx, bitriseapi.ResolveAPIBaseURL(envs), pat)
	if err != nil {
		return "", fmt.Errorf("list workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return "", fmt.Errorf("no workspaces found for this account")
	}
	if len(workspaces) == 1 {
		return workspaces[0].Slug, nil
	}

	items := make([]string, len(workspaces))
	for i, ws := range workspaces {
		items[i] = fmt.Sprintf("%s (%s)", ws.Name, ws.Slug)
	}
	idx, err := selectFromList(cmd, "Select a workspace:", items)
	if err != nil {
		return "", err
	}

	return workspaces[idx].Slug, nil
}

// hydrateStoredAuth makes a prior `login` take effect for every cache command:
// when neither a manual BITRISE_BUILD_CACHE_AUTH_TOKEN nor the CI service JWT is
// present, it refreshes the stored OAuth PAT and exports it (plus the chosen
// workspace) into the environment, so the existing env-based auth resolution
// (common.ReadAuthConfigFromEnvironments) picks it up unchanged. Best-effort:
// any error — including "not logged in" — is ignored, leaving the command to
// surface its own "no auth configured" error. Never overrides env/CI creds, and
// only the local-login fallback ever does a network refresh.
func hydrateStoredAuth(ctx context.Context) {
	if os.Getenv("BITRISE_BUILD_CACHE_AUTH_TOKEN") != "" ||
		os.Getenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN") != "" {
		return
	}
	creds, err := oauth.NewConfigFromEnv(utils.AllEnvs()).EnsureFresh(ctx)
	if err != nil || creds.PAT == "" || creds.WorkspaceID == "" {
		return
	}
	_ = os.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", creds.PAT)
	_ = os.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", creds.WorkspaceID)
}

// isInteractiveStdin reports whether stdin is a terminal (a char device), so
// the workspace picker can read a choice. Pipes/files/CI are not interactive.
func isInteractiveStdin() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return fi.Mode()&os.ModeCharDevice != 0
}
