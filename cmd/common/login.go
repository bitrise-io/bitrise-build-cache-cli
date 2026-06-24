package common

import (
	"context"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/bitriseapi"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
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
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := oauth.Clear(); err != nil {
			return fmt.Errorf("clear stored login: %w", err)
		}
		log.NewLogger(log.WithDebugLog(IsDebugLogMode)).Infof("Signed out.")

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
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))

	if loginWorkspace == "" && !isInteractiveStdin() {
		return fmt.Errorf("not an interactive terminal: pass --workspace <slug> to sign in non-interactively")
	}

	cfg := oauth.NewConfigFromEnv(envs)
	cfg.Logger = logger
	creds, err := cfg.Login(ctx, oauth.OpenBrowser)
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

	logger.Infof("Signed in. Using workspace %q for the build cache.", workspace)

	if shadow := shadowingAuthEnv(); shadow != "" {
		logger.Warnf("%s is set and takes precedence over the login just saved.", shadow)
		logger.Warnf("Build-cache commands will use it, not this login — unset it to use the stored login.")
	}

	return nil
}

// shadowingAuthEnv returns the env credential that takes precedence over the
// stored OAuth login (resolved first in hydrateStoredAuth), or "". Used to warn
// at login that the saved login won't take effect.
func shadowingAuthEnv() string {
	if os.Getenv(configcommon.EnvAuthToken) != "" {
		return configcommon.EnvAuthToken
	}
	if os.Getenv(configcommon.EnvServiceJWT) != "" {
		return configcommon.EnvServiceJWT
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

// hydrateStoredAuth refreshes a stored OAuth login and exports its PAT +
// workspace as the auth env vars when no manual/CI credential is set, so the
// existing env-based resolution picks it up. Best-effort; never overrides
// env/CI creds, and only this fallback ever does a network refresh.
func hydrateStoredAuth(ctx context.Context) {
	if os.Getenv(configcommon.EnvAuthToken) != "" ||
		os.Getenv(configcommon.EnvServiceJWT) != "" {
		return
	}
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))
	cfg := oauth.NewConfigFromEnv(utils.AllEnvs())
	cfg.Logger = logger
	creds, err := cfg.EnsureFresh(ctx)
	if err != nil {
		logger.Debugf("OAuth login not applied: %s", err)

		return
	}
	if creds.PAT == "" || creds.WorkspaceID == "" {
		return
	}
	_ = os.Setenv(configcommon.EnvAuthToken, creds.PAT)
	_ = os.Setenv(configcommon.EnvWorkspaceID, creds.WorkspaceID)
}

// isInteractiveStdin reports whether stdin is a terminal (not a pipe/file/CI).
func isInteractiveStdin() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return fi.Mode()&os.ModeCharDevice != 0
}
