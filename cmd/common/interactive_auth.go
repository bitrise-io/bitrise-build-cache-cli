package common

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// wizardAuth is the credential state the wizard proceeds with.
type wizardAuth struct {
	Config configcommon.CacheAuthConfig
	Source configcommon.AuthSource
	// Stored is the keychain credential behind Config, re-read after a fresh
	// login so persisting a display name later can't drop the OAuth tokens.
	Stored      keychain.Credentials
	SignedInNow bool
}

// NeedsManualPrompt reports that no credential could be resolved, so the wizard
// has to fall back to asking for a workspace ID + token.
func (a wizardAuth) NeedsManualPrompt() bool {
	return a.Source == configcommon.AuthSourceNone
}

// wizardAuthResolver resolves the credential for the interactive wizard. Nil
// function fields fall back to the real OAuth implementations.
type wizardAuthResolver struct {
	Logger   log.Logger
	Keychain keychainStore
	Envs     map[string]string
	// Prompt is where the sign-in confirmation is read from; nil means this
	// session can't confirm one, so no browser is launched.
	Prompt io.Reader

	EnsureFresh func(ctx context.Context) (oauth.Credentials, error)
	Login       func(ctx context.Context) (oauth.Credentials, error)
	// ResolveAuth defaults to configcommon.ResolveAuthConfig, which also reads
	// on-disk credentials; tests override it to stay off the real machine.
	ResolveAuth func(envs map[string]string) (configcommon.CacheAuthConfig, configcommon.AuthSource, error)
}

// Resolve refreshes a stored OAuth login, and when nothing is stored and no env
// vars are set it offers the browser sign-in — after an explicit confirmation,
// so the browser never opens unannounced. Returning AuthSourceNone leaves the
// manual token prompt as the fallback rather than failing the wizard.
func (r wizardAuthResolver) Resolve(ctx context.Context) wizardAuth {
	storedCreds := loadWizardKeychain(r.Logger, r.Keychain)
	cfg, source := wizardStartingCreds(r.Envs, storedCreds, r.ResolveAuth)
	auth := wizardAuth{Config: cfg, Source: source, Stored: storedCreds}

	if source == configcommon.AuthSourceKeychain && storedCreds.IsOAuthManaged() {
		refreshed, err := r.ensureFresh(ctx)
		if err == nil {
			auth.Config.AuthToken, auth.Config.WorkspaceID = refreshed.PAT, refreshed.WorkspaceID
			auth.Stored = loadWizardKeychain(r.Logger, r.Keychain)

			return auth
		}

		r.Logger.Warnf("The stored login could not be refreshed (%v).", err)
		auth.Source = configcommon.AuthSourceNone
	}

	if auth.Source != configcommon.AuthSourceNone {
		return auth
	}

	creds, ok := r.signIn(ctx)
	if !ok {
		return auth
	}

	auth.Config = configcommon.CacheAuthConfig{AuthToken: creds.PAT, WorkspaceID: creds.WorkspaceID}
	auth.Source = configcommon.AuthSourceKeychain
	auth.Stored = loadWizardKeychain(r.Logger, r.Keychain)
	auth.SignedInNow = true

	return auth
}

// signIn confirms with the user, then runs the browser sign-in. ok is false when
// the user declined or the flow failed.
func (r wizardAuthResolver) signIn(ctx context.Context) (oauth.Credentials, bool) {
	if !confirmWizardLogin(r.Logger, r.Prompt) {
		return oauth.Credentials{}, false
	}

	creds, err := r.login(ctx)
	if err != nil {
		r.Logger.Warnf("Browser sign-in did not complete (%v).", err)
		r.Logger.Infof("Falling back to entering a Bitrise personal access token by hand.")

		return oauth.Credentials{}, false
	}

	return creds, true
}

func (r wizardAuthResolver) ensureFresh(ctx context.Context) (oauth.Credentials, error) {
	if r.EnsureFresh != nil {
		return r.EnsureFresh(ctx)
	}

	cfg := oauth.NewConfigFromEnv(r.Envs)
	cfg.Logger = r.Logger

	creds, err := cfg.EnsureFresh(ctx)
	if err != nil {
		return oauth.Credentials{}, fmt.Errorf("refresh stored login: %w", err)
	}

	return creds, nil
}

func (r wizardAuthResolver) login(ctx context.Context) (oauth.Credentials, error) {
	if r.Login != nil {
		return r.Login(ctx)
	}

	return loginAndStore(ctx, r.Logger, r.Envs, "", "")
}

// confirmWizardLogin announces the sign-in and waits for an explicit Enter, so
// the browser is never launched out of nowhere. A nil prompt means there's no
// stream to confirm on (accessible mode owns stdin) — declining there keeps a
// scripted run from opening a browser and then blocking on a callback that
// nothing will ever deliver.
func confirmWizardLogin(logger log.Logger, prompt io.Reader) bool {
	logger.Println()
	logger.TInfof("No Bitrise credentials found on this machine.")
	logger.Infof("Neither %s + %s are set, nor is there a stored login.", configcommon.EnvAuthToken, configcommon.EnvWorkspaceID)

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
func resolvedAuthNote(auth wizardAuth, loader configcommon.AuthLoader) string {
	if auth.SignedInNow || auth.NeedsManualPrompt() {
		return ""
	}

	desc := configcommon.DescribeResolvedWith(auth.Config, auth.Source, loader)

	return "Signing in was not needed — using " + desc.Detail() + "."
}

func loadWizardKeychain(logger log.Logger, kc keychainStore) keychain.Credentials {
	creds, err := kc.Load()
	switch {
	case err == nil, errors.Is(err, keychain.ErrNotFound):
		return creds
	default:
		logger.Warnf("Could not read the OS keychain (%v). Wizard treats it as empty.", err)

		return keychain.Credentials{}
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
