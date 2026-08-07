package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

// loginTimeout bounds the whole browser round-trip.
const loginTimeout = 5 * time.Minute

// The credential helper is spawned N-way parallel by Bazel and WorkOS rotates
// the refresh token on every grant, so two processes spending the same one
// invalidates the login. refreshLockWait stays under the helper's ctx budget.
// Shortened by tests so they don't wait out a real contended lock.
var refreshLockWait = 4 * time.Second //nolint:gochecknoglobals

var (
	ErrNotLoggedIn   = errors.New("not logged in (run 'bitrise-build-cache auth login', or set BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID)")
	ErrLoginRequired = errors.New("OAuth session expired — run 'bitrise-build-cache auth login' to sign in again")
)

// Login runs the browser authorization + token exchange and returns the TokenSet
// (PAT/JWT/refresh/expiries) without WorkspaceID — the caller sets that and
// persists. openBrowser may be nil; the URL is also logged for manual fallback.
func (c Config) Login(ctx context.Context, openBrowser func(string) error) (auth.TokenSet, error) {
	if c.Issuer == "" {
		return auth.TokenSet{}, errors.New("OAuth login is not configured: no issuer (set BITRISE_OAUTH_ISSUER)")
	}
	if c.ClientID == "" {
		return auth.TokenSet{}, errors.New("OAuth login is not configured: no client_id (set BITRISE_OAUTH_CLIENT_ID)")
	}

	state, err := newState()
	if err != nil {
		return auth.TokenSet{}, err
	}
	verifier, challenge, err := newPKCE()
	if err != nil {
		return auth.TokenSet{}, err
	}

	cs, err := newCallbackServer(ctx, state)
	if err != nil {
		return auth.TokenSet{}, err
	}
	defer cs.close() //nolint:contextcheck // close uses a fresh short timeout for cleanup, independent of the (possibly cancelled) login context
	cs.start()

	authURL := c.authorizeURL(challenge, state, cs.redirectURI())
	c.infof("Opening your browser to sign in to Bitrise.")
	c.infof("If it doesn't open automatically, visit:\n\n  %s\n", authURL)
	if c.CallbackFallback != nil {
		c.infof("If the browser can't reach %s after signing in (a connection error —", cs.redirectURI())
		c.infof("expected on a remote/RDE machine, where localhost is not the CLI's host),")
		c.infof("copy the URL from the browser's address bar, paste it here and press Enter.")
	}
	if openBrowser != nil {
		if err := openBrowser(authURL); err != nil {
			c.warnf("Couldn't open the browser automatically: %s", err)
		}
	}

	c.debugf("Waiting for the browser sign-in to complete")
	waitCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	code, err := c.awaitCallback(waitCtx, cs)
	if err != nil {
		return auth.TokenSet{}, err
	}

	c.debugf("Exchanging authorization code for a token")
	now := time.Now() // before the exchange, so the JWT expiry isn't pushed out by the round-trip
	jwtResp, err := c.exchangeCodeForJWT(ctx, code, verifier, cs.redirectURI())
	if err != nil {
		return auth.TokenSet{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	c.debugf("Exchanging token for a Bitrise access token")
	pat, patExpiry, err := c.exchangeJWTForPAT(ctx, jwtResp.AccessToken)
	if err != nil {
		return auth.TokenSet{}, fmt.Errorf("exchange token for a Bitrise PAT: %w", err)
	}
	c.infof("Signed in to Bitrise.")

	return auth.TokenSet{
		AuthToken:    pat,
		PATExpiry:    patExpiry,
		JWT:          jwtResp.AccessToken,
		JWTExpiry:    jwtExpiry(jwtResp, now),
		RefreshToken: jwtResp.RefreshToken,
	}, nil
}

// authorizeURL builds the WorkOS authorize URL. The resource indicator pins the
// JWT audience; offline_access requests a refresh token.
func (c Config) authorizeURL(challenge, state, redirectURI string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.ClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {"openid offline_access"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if c.Resource != "" {
		q.Set("resource", c.Resource)
	}

	return c.authorizeEndpoint() + "?" + q.Encode()
}

// EnsureFresh loads the stored OAuth credential and returns it with a live PAT,
// refreshing without a browser when needed:
//
//	PAT valid             → return it
//	PAT expired           → exchange JWT → new PAT
//	+ JWT expired      → refresh-token grant → new JWT → new PAT
//	refresh token rejected → ErrLoginRequired
//
// Returns ErrNotLoggedIn when no OAuth credential is stored. Persists any new
// tokens back to disk.
func (c Config) EnsureFresh(ctx context.Context) (auth.TokenSet, error) {
	creds, src, err := LoadWithSource()
	if err != nil {
		return auth.TokenSet{}, err
	}

	return c.EnsureFreshFrom(ctx, creds, src)
}

// EnsureFreshFrom is EnsureFresh on a record the caller already loaded, saving
// back to the store it came from. Resolution reads the store once and passes what
// it found, so the refresh cannot act on a different record than the caller saw.
// A nil backing store falls back to the default save target.
func (c Config) EnsureFreshFrom(ctx context.Context, creds auth.TokenSet, src store.Store) (auth.TokenSet, error) {
	save := Save
	if src != nil {
		save = func(cr auth.TokenSet) error { return SaveTo(src, cr) }
	}
	if !creds.IsOAuthManaged() {
		return auth.TokenSet{}, ErrNotLoggedIn
	}

	now := time.Now()
	if creds.AuthToken != "" && now.Add(RefreshSkew).Before(creds.PATExpiry) {
		c.debugf("Stored Bitrise token still valid")

		return creds, nil
	}

	release, lockErr := acquireRefreshLock(ctx)
	if lockErr != nil {
		c.debugf("Refreshing without the cross-process lock: %s", lockErr)
	} else {
		defer func() { _ = release() }()
	}

	// Also after a failed wait: giving up on the lock is precisely the case where
	// someone else is refreshing, and the credential read before the wait is the
	// one whose refresh token they have already rotated away.
	creds, save = reloadStored(creds, save)
	now = time.Now()
	if creds.AuthToken != "" && now.Add(RefreshSkew).Before(creds.PATExpiry) {
		c.debugf("Another process refreshed the Bitrise token")

		return creds, nil
	}

	// PAT stale. If the JWT is still good, a single exchange refreshes the PAT.
	if creds.JWT != "" && now.Add(RefreshSkew).Before(creds.JWTExpiry) {
		if pat, expiry, exErr := c.exchangeJWTForPAT(ctx, creds.JWT); exErr == nil {
			creds.AuthToken, creds.PATExpiry = pat, expiry
			if err := save(creds); err != nil {
				return auth.TokenSet{}, err
			}
			c.infof("Refreshed Bitrise access token.")

			return creds, nil
		}
		// Exchange failed despite an unexpired JWT — fall through to a full refresh.
	}

	if creds.RefreshToken == "" {
		return auth.TokenSet{}, ErrLoginRequired
	}
	c.debugf("Refreshing the OAuth session")
	now = time.Now() // re-anchor to just before the refresh exchange
	refreshed, err := c.refreshJWT(ctx, creds.RefreshToken)
	if err != nil {
		return auth.TokenSet{}, fmt.Errorf("%w (refresh failed: %w)", ErrLoginRequired, err)
	}
	creds.JWT = refreshed.AccessToken
	creds.JWTExpiry = jwtExpiry(refreshed, now)
	if refreshed.RefreshToken != "" { // WorkOS may rotate the refresh token
		creds.RefreshToken = refreshed.RefreshToken
	}

	pat, expiry, err := c.exchangeJWTForPAT(ctx, creds.JWT)
	if err != nil {
		return auth.TokenSet{}, fmt.Errorf("exchange refreshed token for a PAT: %w", err)
	}
	creds.AuthToken, creds.PATExpiry = pat, expiry
	if err := save(creds); err != nil {
		return auth.TokenSet{}, err
	}
	c.infof("Refreshed Bitrise access token.")

	return creds, nil
}
