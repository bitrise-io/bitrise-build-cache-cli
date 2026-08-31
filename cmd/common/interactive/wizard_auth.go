package interactive

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// wizardAuth is the credential state the wizard proceeds with.
type wizardAuth struct {
	Config authpkg.Credential
	// Stored is the keychain credential behind Config, re-read after a fresh
	// login so persisting a display name later can't drop the OAuth tokens.
	Stored      authpkg.TokenSet
	SignedInNow bool
	// Origin is where Config lives. Only meaningful with SignedInNow: a
	// keychain-less host stores a fresh login in the config file, and a later
	// display-name write has to go to the same place.
	Origin authpkg.Origin
	// StdinUnusable means the sign-in left a reader holding stdin, so the wizard
	// must not run any further form.
	StdinUnusable bool
}

// NeedsManualPrompt reports that no credential could be resolved, so the wizard
// has to fall back to asking for a workspace ID + token.
func (a wizardAuth) NeedsManualPrompt() bool {
	return !a.Origin.Resolved()
}

// wizardAuthResolver resolves the credential for the interactive wizard. Nil
// function fields fall back to the real OAuth implementations.
type wizardAuthResolver struct {
	Logger log.Logger
	Store  store.Store
	Envs   map[string]string
	// Prompt is where the sign-in confirmation is read from; nil means this
	// session can't confirm one, so no browser is launched.
	Prompt io.Reader
	// Workspace skips the workspace picker, which a sign-in that has taken over
	// stdin cannot show.
	Workspace string

	// EnsureFresh overrides the refresh step; nil means the real OAuth flow.
	EnsureFresh func(ctx context.Context) (authpkg.TokenSet, error)
	Login       func(ctx context.Context) (authpkg.TokenSet, error)
	// Resolver defaults to a PreferStored resolver, which also reads on-disk
	// credentials; tests override it to stay off the real machine.
	Resolver *live.Resolver
}

// Resolve refreshes a stored OAuth login, and when nothing is stored and no env
// vars are set it offers the browser sign-in — after an explicit confirmation,
// so the browser never opens unannounced. An unresolved Origin leaves the manual
// token prompt as the fallback rather than failing the wizard.
func (r wizardAuthResolver) Resolve(ctx context.Context) wizardAuth {
	storedCreds := loadStoredCredentials(r.Logger, r.Store)

	// PreferStored: a populated keychain must not be shadowed by a stale token
	// exported from a shell rc file on the machine in front of the user.
	// FailFast: with a user present, a dead refresh token means "offer the sign-in",
	// not "serve a token the backend will reject". A transient refresh error
	// (network blip, cancelled ctx) is different: the stored token may still be
	// live, and forcing a new sign-in there is what turned this into a two-run
	// wizard.
	cred, origin, err := r.resolver().Resolve(ctx, r.Envs)
	switch {
	case err == nil:
	case errors.Is(err, oauth.ErrLoginRequired), !origin.StoreManaged():
		r.Logger.Warnf("The stored login could not be refreshed (%v).", err)
		cred, origin = authpkg.Credential{}, authpkg.Origin{}
	default:
		r.Logger.Warnf("Could not refresh the stored login right now (%v); using the credential as-is.", err)
	}
	auth := wizardAuth{Config: cred, Origin: origin, Stored: storedCreds}

	if origin.StoreManaged() {
		auth.Stored = loadStoredCredentials(r.Logger, r.Store)
	}

	if auth.Origin.Resolved() {
		return auth
	}

	out, ok := r.signIn(ctx)
	if !ok {
		return auth
	}

	auth.Config = out.Creds.Credential()
	auth.Origin = out.Origin
	auth.Stored = loadStoredCredentials(r.Logger, r.storeFor(out.Origin.Backend))
	auth.SignedInNow = true
	auth.StdinUnusable = out.StdinUnusable

	return auth
}

func (r wizardAuthResolver) resolver() *live.Resolver {
	res := r.Resolver
	if res == nil {
		res = live.Default(r.Logger)
		res.Prefer = live.PreferStored
	}
	res.OnRefreshFailure = live.FailFast
	if r.EnsureFresh != nil && res.Refresh == nil {
		res.Refresh = func(ctx context.Context, _ authpkg.TokenSet, _ store.Store) (authpkg.TokenSet, error) {
			return r.EnsureFresh(ctx)
		}
	}

	return res
}

// storeFor returns the backend to read and update, honouring an injected store so
// tests stay off the real one.
func (r wizardAuthResolver) storeFor(backend authpkg.Backend) store.Store {
	if backend == authpkg.BackendFile {
		return store.NewFile()
	}
	if r.Store != nil {
		return r.Store
	}

	return store.NewKeychain()
}

// signIn confirms with the user, then runs the browser sign-in. ok is false when
// the user declined or the flow failed.
func (r wizardAuthResolver) signIn(ctx context.Context) (loginOutcome, bool) {
	if !confirmWizardLogin(r.Logger, r.Prompt) {
		return loginOutcome{}, false
	}

	out, err := r.login(ctx)
	switch {
	case errors.Is(err, ErrStdinUnusable):
		// The credential was saved; what's gone is the ability to prompt. Falling
		// back to the token prompt would just drop keystrokes.
		out.StdinUnusable = true

		return out, true
	case err != nil:
		r.Logger.Warnf("Browser sign-in did not complete (%v).", err)
		r.Logger.Infof("Falling back to entering a Bitrise personal access token by hand.")

		return loginOutcome{}, false
	}

	return out, true
}

func (r wizardAuthResolver) login(ctx context.Context) (loginOutcome, error) {
	if r.Login != nil {
		creds, err := r.Login(ctx)

		return loginOutcome{Creds: creds, Origin: creds.Origin(authpkg.BackendKeychain)}, err
	}

	return loginAndStore(ctx, r.Logger, r.Envs, loginRequest{
		Workspace:     workspaceChoice{Slug: r.Workspace},
		WorkspaceFlag: wizardWorkspaceFlag,
	})
}

// confirmWizardLogin announces the sign-in and waits for an explicit Enter, so
// the browser is never launched out of nowhere. A nil prompt means there's no
// stream to confirm on (accessible mode owns stdin) — declining there keeps a
// scripted run from opening a browser and then blocking on a callback that
// nothing will ever deliver.
func confirmWizardLogin(logger log.Logger, prompt io.Reader) bool {
	logger.Println()
	logger.TInfof("No Bitrise credentials found on this machine.")
	logger.Infof("Neither %s + %s are set, nor is there a stored login.", authpkg.EnvAuthToken, authpkg.EnvWorkspaceID)

	if prompt == nil {
		logger.Infof("Not asking for a browser sign-in here — this session can't confirm it.")
		logger.Infof("Run `%s auth login` for the browser flow, or enter a token below.", paths.CLIBinaryName)

		return false
	}

	logger.Infof("The next step signs you in to Bitrise in your browser.")
	logger.Println()
	logger.Infof("Press Enter to open the browser, or 's' + Enter to skip and type a token by hand.")

	line, err := bufio.NewReader(prompt).ReadString('\n')
	if err != nil && line == "" {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "skip", "n", "no":
		return false
	}

	return true
}

// resolvedAuthNote describes credentials the wizard found without asking, so the
// first screen says where they came from. Empty when the user just signed in
// (they know) or when none were found (the token prompt follows instead).
func resolvedAuthNote(auth wizardAuth) string {
	if auth.SignedInNow || auth.NeedsManualPrompt() {
		return ""
	}

	return "Signing in was not needed — using " + live.Describe(auth.Config, auth.Origin) + "."
}

func loadStoredCredentials(logger log.Logger, s store.Store) authpkg.TokenSet {
	creds, err := s.Load()
	switch {
	// store.ErrNotFound covers both "nothing stored" and "no keyring on this host",
	// which is a supported setup rather than a fault — the wizard has no business
	// warning about it, and the backend's own sentinels are not its concern.
	case err == nil, errors.Is(err, store.ErrNotFound):
		return creds
	default:
		logger.Warnf("Could not read the credential store (%v). Wizard treats it as empty.", err)

		return authpkg.TokenSet{}
	}
}

// wizardPromptReader is stdin when huh isn't reading it. The TERM=dumb
// accessible path pipes its answers in and that stream belongs to the form, so
// there's nothing left to confirm a browser sign-in on.
func wizardPromptReader() io.Reader {
	if os.Getenv("TERM") == "dumb" {
		return nil
	}

	return os.Stdin
}
