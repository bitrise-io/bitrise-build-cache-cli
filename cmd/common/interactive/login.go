package interactive

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/bitriseapi"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
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
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

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
			logger.Infof("A manually set token is stored in the %s and is left untouched — use `auth clear` to remove that.", source.Backend())
		default:
			// Only the backend holding the login: oauth.Clear() wipes both, which
			// would take a manually set token in the other one with it — and
			// PersistActivateCreds writes without SaveExclusive, so both can be
			// populated at once.
			if err := oauth.ClearFrom(source); err != nil {
				return fmt.Errorf("clear stored login: %w", err)
			}
			logger.Infof("Signed out — the browser login was removed from the %s.", creds.Origin(source.Backend()).Label())
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
	logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

	if loginWorkspace == "" && !isInteractiveStdin() {
		return fmt.Errorf("not an interactive terminal: pass --workspace <slug> to sign in non-interactively")
	}

	out, err := loginAndStore(ctx, logger, envs, loginWorkspace, loginStorage, "--workspace")
	switch {
	case errors.Is(err, tui.ErrAborted):
		logger.Infof("Sign-in cancelled. Nothing was saved.")

		return nil
	case errors.Is(err, ErrStdinUnusable):
		return reportStdinUnusable(ctx, logger, envs, out, "bitrise-build-cache auth login --workspace <slug>")
	case err != nil:
		return err
	}

	if shadow := shadowingAuthEnv(); shadow != "" {
		logger.Warnf("%s is set and takes precedence over the login just saved.", shadow)
		logger.Warnf("Build-cache commands will use it, not this login — unset it to use the stored login.")
	}

	return nil
}

// ErrStdinUnusable means the sign-in left the paste reader holding stdin and it
// could not be stopped, so no further prompt in this process can be trusted with
// keystrokes. Callers report a way to continue without one.
var ErrStdinUnusable = errors.New("standard input is no longer usable for prompts")

// loginOutcome is what a completed sign-in leaves behind. Origin matters because a
// keychain-less host stores the login in the config file instead, and callers
// that go on to describe or update the credential have to target the right one.
type loginOutcome struct {
	Creds  auth.TokenSet
	Origin auth.Origin
	// StdinUnusable means a paste reader is still holding stdin, so no prompt in
	// this process can be trusted with keystrokes.
	StdinUnusable bool
}

// loginAndStore runs the browser OAuth flow, resolves the workspace and persists
// the credential. workspace empty → interactive picker; storage empty → default
// target for the environment. Shared by `auth login` and the interactive wizard.
//
// workspaceFlag names the flag a caller can use to supply the workspace without
// a prompt; it appears in ErrStdinUnusable guidance and differs per command.
func loginAndStore(ctx context.Context, logger log.Logger, envs map[string]string, workspace, storage, workspaceFlag string) (loginOutcome, error) {
	cfg := oauth.NewConfigFromEnv(envs)
	cfg.Logger = logger

	paster := &callbackPaster{Reader: os.Stdin, Logger: logger, WorkspaceFlag: workspaceFlag}
	if isInteractiveStdin() {
		cfg.CallbackFallback = paster.Fallback
	}

	creds, err := cfg.Login(ctx, oauth.OpenBrowser)
	if err != nil {
		return loginOutcome{}, fmt.Errorf("sign in: %w", err)
	}

	if workspace == "" {
		// A picker here would race the paste reader for every keystroke, which is
		// the failure this reports instead of reproducing.
		if paster.StdinUnusable() {
			return loginOutcome{}, fmt.Errorf("%w: cannot show the workspace picker", ErrStdinUnusable)
		}

		workspace, err = pickWorkspace(ctx, envs, creds.AuthToken)
		if err != nil {
			return loginOutcome{}, err
		}
	}
	creds.WorkspaceID = workspace

	target, err := store.Select(configcommon.DetectCIProvider(envs) != "", storage)
	if err != nil {
		return loginOutcome{}, err //nolint:wrapcheck
	}

	origin, err := saveLoginWithFallback(logger, target, storage, creds)
	if err != nil {
		return loginOutcome{}, err
	}

	logger.Infof("Signed in. Using workspace %q for the build cache. Credentials stored in the %s.", workspace, origin.Label())

	out := loginOutcome{Creds: creds, Origin: origin, StdinUnusable: paster.StdinUnusable()}
	if out.StdinUnusable {
		return out, fmt.Errorf("%w: the credential was saved", ErrStdinUnusable)
	}

	return out, nil
}

// saveLoginWithFallback stores the login, dropping to the multiplatform config
// when the keychain refuses the write — a locked or unavailable keychain
// shouldn't throw away a completed sign-in. The whole credential goes to the
// fallback, refresh token included, so the login stays refreshable there.
//
// An explicit --storage choice is honoured: the caller asked for that backend.
func saveLoginWithFallback(logger log.Logger, target store.Store, storage string, creds auth.TokenSet) (auth.Origin, error) {
	result, err := oauth.SaveToWithFallback(target, creds, storage == "")
	if err != nil {
		return result.Origin, fmt.Errorf("save credentials: %w", err)
	}
	result.WarnFallback(logger)

	return result.Origin, nil
}

// shadowingAuthEnv returns the env var that shadows the stored login, or "".
func shadowingAuthEnv() string {
	switch _, origin, _ := live.Default(nil).ResolveNoRefresh(utils.AllEnvs()); origin.Backend {
	case auth.BackendEnv:
		return auth.EnvAuthToken
	case auth.BackendJWT:
		return auth.EnvJWT
	case auth.BackendNone, auth.BackendKeychain, auth.BackendFile:
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

// isInteractiveStdin reports whether stdin is a terminal (not a pipe/file/CI).
func isInteractiveStdin() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return fi.Mode()&os.ModeCharDevice != 0
}

// reportStdinUnusable explains how to finish without a prompt, listing the
// workspaces when the credential is good enough to ask the API for them — the
// user needs a slug to pass, and the picker that would have shown it is exactly
// what is unavailable.
func reportStdinUnusable(ctx context.Context, logger log.Logger, envs map[string]string, out loginOutcome, rerun string) error {
	logger.Warnf("The sign-in took long enough that the CLI had started reading standard input, and it cannot stop.")
	logger.Warnf("Any prompt now would drop keystrokes, so this command stops here.")

	if out.Creds.AuthToken != "" {
		if workspaces, err := bitriseapi.ListWorkspaces(ctx, bitriseapi.ResolveAPIBaseURL(envs), out.Creds.AuthToken); err == nil {
			logger.Infof("Workspaces you can use:")
			for _, ws := range workspaces {
				logger.Infof("  %s (%s)", ws.Name, ws.Slug)
			}
		}
	}

	logger.Infof("Run: %s", rerun)

	return ErrStdinUnusable
}
