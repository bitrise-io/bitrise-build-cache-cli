package common

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/bitriseapi"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

//nolint:gochecknoglobals
var (
	loginWorkspace string
	loginStorage   string
)

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
	Short: "Sign out of the browser login (leaves a manually set token in place)",
	Long: `Sign out of the browser (OAuth) login: removes the stored login and its
refresh token, so build-cache commands stop using it.

This is narrower than ` + "`auth clear`" + `:

  auth logout   removes only the browser login. A token you set yourself with
                ` + "`auth set`" + ` is left alone, and stays in use.
  auth clear    removes every stored credential — the browser login and any
                manually set token — from both the keychain and the config file.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))

		creds, source, err := oauth.LoadWithSource()
		if err != nil {
			return fmt.Errorf("read stored login: %w", err)
		}

		switch {
		case source == nil:
			logger.Infof("Not signed in, and no credentials are stored — nothing to do.")
			logger.Infof("`auth clear` is the command for removing a manually set token.")
		case !creds.IsOAuthManaged():
			logger.Infof("Not signed in via the browser, so there is no login to remove.")
			logger.Infof("A manually set token is stored in the %s and is left untouched — use `auth clear` to remove that.", source.Kind())
		default:
			if err := oauth.Clear(); err != nil {
				return fmt.Errorf("clear stored login: %w", err)
			}
			logger.Infof("Signed out — the browser login was removed from the %s.", source.Kind())
			logger.Infof("Any token set with `auth set` is unaffected.")
		}

		return nil
	},
}

func init() { //nolint:gochecknoinits
	LoginCmd.Flags().StringVar(&loginWorkspace, "workspace", "", "workspace (organization) slug to use; skips the interactive picker")
	LoginCmd.Flags().StringVar(&loginStorage, "storage", "", "Where to persist credentials: keychain | file | auto (default: CI→file, local→keychain).")
	// LoginCmd / LogoutCmd are registered under the `auth` command (cmd/auth).
}

func runLogin(cmd *cobra.Command) error {
	ctx := cmd.Context()
	envs := utils.AllEnvs()
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))

	if loginWorkspace == "" && !isInteractiveStdin() {
		return fmt.Errorf("not an interactive terminal: pass --workspace <slug> to sign in non-interactively")
	}

	if _, err := loginAndStore(ctx, logger, envs, loginWorkspace, loginStorage); err != nil {
		if errors.Is(err, ErrAborted) {
			logger.Infof("Sign-in cancelled. Nothing was saved.")

			return nil
		}

		return err
	}

	if shadow := shadowingAuthEnv(); shadow != "" {
		logger.Warnf("%s is set and takes precedence over the login just saved.", shadow)
		logger.Warnf("Build-cache commands will use it, not this login — unset it to use the stored login.")
	}

	return nil
}

// loginAndStore runs the browser OAuth flow, resolves the workspace and persists
// the credential. workspace empty → interactive picker; storage empty → default
// target for the environment. Shared by `auth login` and the interactive wizard.
func loginAndStore(ctx context.Context, logger log.Logger, envs map[string]string, workspace, storage string) (oauth.Credentials, error) {
	cfg := oauth.NewConfigFromEnv(envs)
	cfg.Logger = logger
	if isInteractiveStdin() {
		cfg.PasteReader = os.Stdin
	}

	creds, err := cfg.Login(ctx, oauth.OpenBrowser)
	if err != nil {
		return oauth.Credentials{}, fmt.Errorf("sign in: %w", err)
	}

	if workspace == "" {
		workspace, err = pickWorkspace(ctx, envs, creds.PAT)
		if err != nil {
			return oauth.Credentials{}, err
		}
	}
	creds.WorkspaceID = workspace

	target, err := store.Select(envs, storage)
	if err != nil {
		return oauth.Credentials{}, err //nolint:wrapcheck
	}

	kind, err := saveLoginWithFallback(logger, target, storage, creds)
	if err != nil {
		return oauth.Credentials{}, err
	}

	switch kind {
	case store.KindKeychain:
		logger.Infof("Signed in. Using workspace %q for the build cache. Credentials stored in the OS keychain.", workspace)
	case store.KindFile:
		logger.Infof("Signed in. Using workspace %q for the build cache. Credentials stored in the multiplatform config file (CI-safe).", workspace)
	}

	return creds, nil
}

// saveLoginWithFallback stores the login, dropping to the multiplatform config
// when the keychain refuses the write — a locked or unavailable keychain
// shouldn't throw away a completed sign-in. The whole credential goes to the
// fallback, refresh token included, so the login stays refreshable there.
//
// An explicit --storage choice is honoured: the caller asked for that backend.
func saveLoginWithFallback(logger log.Logger, target store.Store, storage string, creds oauth.Credentials) (store.Kind, error) {
	err := oauth.SaveTo(target, creds)
	if err == nil {
		return target.Kind(), nil
	}

	if target.Kind() != store.KindKeychain || storage != "" {
		return target.Kind(), fmt.Errorf("save credentials: %w", err)
	}

	logger.Warnf("Could not write to the OS keychain (%s).", err)

	fallback := store.NewFile()
	if fbErr := oauth.SaveTo(fallback, creds); fbErr != nil {
		return fallback.Kind(), fmt.Errorf("save credentials to the keychain (%w) and to the config file (%w)", err, fbErr)
	}

	return fallback.Kind(), nil
}

// shadowingAuthEnv returns the env var that shadows the stored login, or "".
func shadowingAuthEnv() string {
	switch _, source, _ := configcommon.ResolveAuthConfig(utils.AllEnvs()); source {
	case configcommon.AuthSourceEnvVars:
		return configcommon.EnvAuthToken
	case configcommon.AuthSourceJWT:
		return configcommon.EnvJWT
	case configcommon.AuthSourceNone, configcommon.AuthSourceKeychain, configcommon.AuthSourceFile, configcommon.AuthSourceMultiplatform:
	}

	return ""
}

// pickWorkspace lists the workspaces the fresh PAT can access and lets the user
// choose one (auto-selecting when there's exactly one).
func pickWorkspace(ctx context.Context, envs map[string]string, pat string) (string, error) {
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
	idx, err := selectFromList("Select a workspace:", items)
	if err != nil {
		return "", err
	}

	return workspaces[idx].Slug, nil
}

// Skip on Bitrise CI where JWT is env-injected; self-hosted CI with a stored PAT still refreshes.
func hydrateStoredAuth(ctx context.Context) {
	envs := utils.AllEnvs()
	if envs[configcommon.EnvJWT] != "" {
		return
	}
	_, source, _ := configcommon.ResolveAuthConfig(envs)
	if source != configcommon.AuthSourceKeychain && source != configcommon.AuthSourceFile {
		return
	}
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))
	cfg := oauth.NewConfigFromEnv(utils.AllEnvs())
	cfg.Logger = logger
	if _, err := cfg.EnsureFresh(ctx); err != nil {
		logger.Debugf("OAuth login not applied: %s", err)
	}
}

// isInteractiveStdin reports whether stdin is a terminal (not a pipe/file/CI).
func isInteractiveStdin() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return fi.Mode()&os.ModeCharDevice != 0
}
