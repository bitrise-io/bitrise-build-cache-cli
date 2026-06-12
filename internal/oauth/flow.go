package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"
)

// loginTimeout bounds the whole browser round-trip.
const loginTimeout = 5 * time.Minute

var (
	// ErrNotLoggedIn is returned by EnsureFresh when there is no stored OAuth
	// credential (the user hasn't run `login`).
	ErrNotLoggedIn = errors.New("not logged in (run 'bitrise-build-cache login', or set BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID)")
	// ErrLoginRequired is returned when an OAuth credential can no longer be
	// refreshed and the user must sign in again.
	ErrLoginRequired = errors.New("OAuth session expired — run 'bitrise-build-cache login' to sign in again")
)

// Login runs the full browser authorization + token exchange and returns
// Credentials populated with the PAT, JWT, refresh token, and expiries — but
// NOT WorkspaceID (the caller picks a workspace, sets it, then Save). It does
// not persist anything. openBrowser opens the authorize URL (nil to skip
// auto-open); progress + the URL are written to stderr for manual fallback.
func (c Config) Login(ctx context.Context, openBrowser func(string) error, stderr io.Writer) (Credentials, error) {
	if c.Issuer == "" {
		return Credentials{}, errors.New("OAuth login is not configured: no issuer (set BITRISE_OAUTH_ISSUER)")
	}
	if c.ClientID == "" {
		return Credentials{}, errors.New("OAuth login is not configured: no client_id (set BITRISE_OAUTH_CLIENT_ID)")
	}

	state, err := newState()
	if err != nil {
		return Credentials{}, err
	}
	verifier, challenge, err := newPKCE()
	if err != nil {
		return Credentials{}, err
	}

	cs, err := newCallbackServer(ctx, state)
	if err != nil {
		return Credentials{}, err
	}
	defer cs.close() //nolint:contextcheck // close uses a fresh short timeout for cleanup, independent of the (possibly cancelled) login context
	cs.start()

	authURL := c.authorizeURL(challenge, state, cs.redirectURI())
	if _, err := fmt.Fprintf(stderr, "Opening your browser to sign in to Bitrise.\nIf it doesn't open automatically, visit:\n\n  %s\n\n", authURL); err != nil {
		return Credentials{}, fmt.Errorf("write sign-in prompt: %w", err)
	}
	if openBrowser != nil {
		if err := openBrowser(authURL); err != nil {
			if _, werr := fmt.Fprintf(stderr, "(couldn't open the browser automatically: %v)\n", err); werr != nil {
				return Credentials{}, fmt.Errorf("write browser-open notice: %w", werr)
			}
		}
	}

	waitCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	code, err := cs.wait(waitCtx)
	if err != nil {
		return Credentials{}, err
	}

	jwtResp, err := c.exchangeCodeForJWT(ctx, code, verifier, cs.redirectURI())
	if err != nil {
		return Credentials{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	pat, patExpiry, err := c.exchangeJWTForPAT(ctx, jwtResp.AccessToken)
	if err != nil {
		return Credentials{}, fmt.Errorf("exchange token for a Bitrise PAT: %w", err)
	}

	now := time.Now()

	return Credentials{
		PAT:          pat,
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
func (c Config) EnsureFresh(ctx context.Context) (Credentials, error) {
	creds, err := Load()
	if err != nil {
		return Credentials{}, err
	}
	if !creds.IsOAuthManaged() {
		return Credentials{}, ErrNotLoggedIn
	}

	now := time.Now()
	if creds.PAT != "" && now.Add(refreshSkew).Before(creds.PATExpiry) {
		return creds, nil
	}

	// PAT stale. If the JWT is still good, a single exchange refreshes the PAT.
	if creds.JWT != "" && now.Add(refreshSkew).Before(creds.JWTExpiry) {
		if pat, expiry, exErr := c.exchangeJWTForPAT(ctx, creds.JWT); exErr == nil {
			creds.PAT, creds.PATExpiry = pat, expiry
			if err := Save(creds); err != nil {
				return Credentials{}, err
			}

			return creds, nil
		}
		// Exchange failed despite an unexpired JWT — fall through to a full refresh.
	}

	if creds.RefreshToken == "" {
		return Credentials{}, ErrLoginRequired
	}
	refreshed, err := c.refreshJWT(ctx, creds.RefreshToken)
	if err != nil {
		return Credentials{}, fmt.Errorf("%w (refresh failed: %w)", ErrLoginRequired, err)
	}
	creds.JWT = refreshed.AccessToken
	creds.JWTExpiry = jwtExpiry(refreshed, now)
	if refreshed.RefreshToken != "" { // WorkOS may rotate the refresh token
		creds.RefreshToken = refreshed.RefreshToken
	}

	pat, expiry, err := c.exchangeJWTForPAT(ctx, creds.JWT)
	if err != nil {
		return Credentials{}, fmt.Errorf("exchange refreshed token for a PAT: %w", err)
	}
	creds.PAT, creds.PATExpiry = pat, expiry
	if err := Save(creds); err != nil {
		return Credentials{}, err
	}

	return creds, nil
}
