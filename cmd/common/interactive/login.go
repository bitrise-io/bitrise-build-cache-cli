package interactive

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	loginWorkspace   string
	loginStorage     string
	loginNoWorkspace bool
	loginPrintURL    bool
	loginCallback    string
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

The sign-in is handed back over a loopback address, so it expects a browser on
the same machine. On a remote/RDE session the redirect fails; paste the URL from
the browser's address bar into the terminal and the sign-in completes from that.

A session with no terminal at all (an agent driving the CLI, a script) can't be
prompted for a workspace: pass --workspace <slug>, or --no-workspace to sign in
first and select afterwards with ` + "`auth workspace --set <slug>`" + `. It can't paste
into a prompt either, so the sign-in is driven in two commands instead:

  bitrise-build-cache auth login --print-url --no-workspace   # prints the URL, waits
  bitrise-build-cache auth login --callback '<address>'       # from another shell

Open the printed URL in a browser anywhere, sign in, and hand the address it
lands on — the one that fails to load — to the second command, on this machine.`,
	SilenceUsage: true,
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
	// No backticks in these usage strings: pflag reads the first backticked word as
	// the value placeholder, which turns the help into nonsense.
	LoginCmd.Flags().BoolVar(&loginNoWorkspace, "no-workspace", false, "Sign in without selecting a workspace. Pick one afterwards with 'auth workspace --list' + 'auth workspace --set <slug>'; build-cache commands stay unconfigured until you do.")
	LoginCmd.Flags().BoolVar(&loginPrintURL, "print-url", false, "Don't launch a browser: print the sign-in URL (bare, on stdout) and wait. Open it wherever you have a browser, then deliver the address it lands on with 'auth login --callback <url>'.")
	LoginCmd.Flags().StringVar(&loginCallback, "callback", "", "Deliver a callback URL copied from the browser to the 'auth login --print-url' waiting on this machine, and exit. Signs nothing in by itself.")
	// LoginCmd / LogoutCmd are registered under the `auth` command (cmd/auth).
}

func runLogin(cmd *cobra.Command) error {
	ctx := cmd.Context()
	envs := utils.AllEnvs()
	logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

	if loginCallback != "" {
		if loginWorkspace != "" || loginNoWorkspace || loginPrintURL {
			return fmt.Errorf("--callback only delivers a URL to a waiting sign-in; it takes no other flag")
		}

		return relayCallback(ctx, logger, loginCallback)
	}

	if err := validateLoginWorkspaceFlags(loginWorkspace, loginNoWorkspace, isInteractiveStdin()); err != nil {
		return err
	}

	req := loginRequest{
		Workspace:     workspaceChoice{Slug: loginWorkspace, Skip: loginNoWorkspace},
		Storage:       loginStorage,
		WorkspaceFlag: "--workspace",
	}
	if loginPrintURL {
		req.PrintURL = cmd.OutOrStdout()
	}

	out, err := loginAndStore(ctx, logger, envs, req)
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

func validateLoginWorkspaceFlags(workspace string, noWorkspace, interactive bool) error {
	if workspace != "" && noWorkspace {
		return fmt.Errorf("--workspace and --no-workspace are mutually exclusive")
	}
	if workspace == "" && !noWorkspace && !interactive {
		return fmt.Errorf("not an interactive terminal: pass --workspace <slug>, or --no-workspace to pick one afterwards with `auth workspace --set`")
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

// Test seam: the integration test stands in a client that follows the redirect.
var openBrowser = defaultOpenBrowser //nolint:gochecknoglobals

func defaultOpenBrowser(url string) error {
	return oauth.OpenBrowser(url) //nolint:wrapcheck // oauth already wraps
}

// The zero value means the interactive picker.
type workspaceChoice struct {
	Slug string
	// Skip stores the login workspace-less, for callers that cannot show a picker.
	Skip bool
}

type loginRequest struct {
	Workspace workspaceChoice
	// Storage empty → the default target for this environment.
	Storage string
	// WorkspaceFlag names the flag this caller accepts instead of a prompt; it
	// appears in ErrStdinUnusable guidance. Empty means it has none to offer.
	WorkspaceFlag string
	// PrintURL, when set, receives the sign-in URL and suppresses the browser.
	PrintURL io.Writer
}

// loginAndStore runs the browser OAuth flow, resolves the workspace and persists
// the credential. Shared by `auth login`, the wizard and the doctor's fixer.
func loginAndStore(ctx context.Context, logger log.Logger, envs map[string]string, req loginRequest) (loginOutcome, error) {
	cfg := oauth.NewConfigFromEnv(envs)
	cfg.Logger = logger

	paster := &callbackPaster{Reader: os.Stdin, Logger: logger, WorkspaceFlag: req.WorkspaceFlag}
	if isInteractiveStdin() {
		cfg.CallbackFallback = paster.Fallback
	}

	open := openBrowser
	if req.PrintURL != nil {
		open = nil
		cfg.OnAuthorizeURL = func(authorizeURL, redirectURI string) {
			_, _ = fmt.Fprintln(req.PrintURL, authorizeURL)
			logger.Infof("After signing in, the browser lands on %s, which only this machine can reach.", redirectURI)
			logger.Infof("Copy that address and deliver it here with:")
			logger.Infof("  bitrise-build-cache auth login --callback '<address>'")
		}
	}

	creds, err := cfg.Login(ctx, open)
	if err != nil {
		return loginOutcome{}, fmt.Errorf("sign in: %w", err)
	}

	workspace := req.Workspace.Slug
	if workspace == "" && !req.Workspace.Skip {
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
	// A fresh sign-in carries no display name, and the save is exclusive — without
	// this, `auth login` silently drops the name `auth username` set.
	creds.Username = store.StoredUsername()

	target, err := store.Select(configcommon.DetectCIProvider(envs) != "", req.Storage)
	if err != nil {
		return loginOutcome{}, err //nolint:wrapcheck
	}

	origin, err := saveLoginWithFallback(logger, target, req.Storage, creds)
	if err != nil {
		return loginOutcome{}, err
	}

	if workspace == "" {
		logger.Infof("Signed in. Credentials stored in the %s.", origin.Label())
		logger.Infof("No workspace selected yet — the build cache stays unconfigured until you pick one:")
		logger.Infof("  bitrise-build-cache auth workspace --list")
		logger.Infof("  bitrise-build-cache auth workspace --set <slug>")
	} else {
		logger.Infof("Signed in. Using workspace %q for the build cache. Credentials stored in the %s.", workspace, origin.Label())
	}

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

// PickWorkspacePrompt selects and stores a workspace for a login that has none,
// for the doctor's fixer.
func PickWorkspacePrompt(ctx context.Context, logger log.Logger) func() (string, error) {
	return func() (string, error) {
		envs := utils.AllEnvs()

		cred, _, err := live.Default(logger).ResolveTokenOnly(ctx, envs)
		if err != nil {
			return "", fmt.Errorf("resolve the stored credential: %w", err)
		}

		workspace, err := pickWorkspace(ctx, envs, cred.Token)
		if err != nil {
			return "", err
		}

		origin, err := store.SetWorkspaceID(configcommon.DetectCIProvider(envs) != "", workspace)
		if err != nil {
			return "", err //nolint:wrapcheck // already user-facing
		}
		logger.TInfof("✅ Using workspace %q for the build cache (stored in the %s).", workspace, origin.Label())

		return workspace, nil
	}
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
