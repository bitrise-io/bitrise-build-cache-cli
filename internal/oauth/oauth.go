// Package oauth implements the browser-based OAuth login for the Bitrise Build
// Cache CLI and the transparent token refresh that keeps it working, plus the
// on-disk store for the resulting credential.
//
// The CLI is a public OAuth client (no client secret — PKCE replaces it). The
// login is the standard authorization-code + PKCE flow against Bitrise's WorkOS
// AuthKit environment over a loopback redirect (RFC 8252): authorize → exchange
// the code for a JWT at <issuer>/oauth2/token → exchange that JWT for a Bitrise
// PAT at the monolith's OIDC token endpoint (RFC 8693). The PAT is the working
// credential the cache backend already accepts (same as a manually-set
// BITRISE_BUILD_CACHE_AUTH_TOKEN); the JWT + refresh token + expiries are stored
// alongside it so the PAT can be refreshed without a browser.
//
// This is a port of bitrise-cli's internal/oauth, adapted for this CLI's
// local-login use-case (the credential is stored here, with a workspace ID).
// None of the identity inputs are secret, so they ship in the binary.
package oauth

import (
	"net/http"
	"strings"
	"time"
)

// Identity defaults (production). All overridable per environment via the
// matching env vars in NewConfigFromEnv; none is a secret. They reuse the
// bitrise-cli WorkOS client identity, so no new WorkOS/monolith setup is needed.
const (
	DefaultIssuer       = "https://oauth.bitrise.io"
	DefaultClientID     = "https://app.bitrise.io/.well-known/oauth-client/cli"
	DefaultOIDCEndpoint = "https://app.bitrise.io/oidc/token"
	// DefaultResource is the audience/resource indicator pinned into the JWT;
	// it must be registered as a Resource Indicator in WorkOS (it already is,
	// for bitrise-cli). The monolith accepts any *.bitrise.io audience.
	DefaultResource = "https://cli.bitrise.io"
)

// defaultTimeout bounds each token HTTP call. defaultPATLifetime is the
// fallback PAT lifetime when the exchange omits expires_in. refreshSkew
// re-mints slightly before expiry so a token never goes stale mid-request.
const (
	defaultTimeout     = 30 * time.Second
	defaultPATLifetime = time.Hour
	refreshSkew        = 60 * time.Second
)

// Config carries the external inputs for the OAuth flow. Build one with
// NewConfigFromEnv; tests construct it directly with their own httptest URLs.
type Config struct {
	Issuer            string // WorkOS AuthKit domain hosting /oauth2/authorize and /oauth2/token
	OIDCTokenEndpoint string // monolith endpoint that exchanges a JWT for a PAT
	ClientID          string // CIMD URL identifying this client
	Resource          string // audience/resource indicator pinned into the JWT
	HTTPClient        *http.Client
}

// NewConfigFromEnv builds a Config from the compile-time defaults, each
// overridable via env (to target a non-prod environment):
// BITRISE_OAUTH_ISSUER, BITRISE_OIDC_TOKEN_ENDPOINT, BITRISE_OAUTH_CLIENT_ID.
func NewConfigFromEnv(envs map[string]string) Config {
	return Config{
		Issuer:            firstNonEmpty(envs["BITRISE_OAUTH_ISSUER"], DefaultIssuer),
		OIDCTokenEndpoint: firstNonEmpty(envs["BITRISE_OIDC_TOKEN_ENDPOINT"], DefaultOIDCEndpoint),
		ClientID:          firstNonEmpty(envs["BITRISE_OAUTH_CLIENT_ID"], DefaultClientID),
		Resource:          DefaultResource,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

func (c Config) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}

	return &http.Client{Timeout: defaultTimeout}
}

func (c Config) authorizeEndpoint() string {
	return strings.TrimRight(c.Issuer, "/") + "/oauth2/authorize"
}

func (c Config) tokenEndpoint() string {
	return strings.TrimRight(c.Issuer, "/") + "/oauth2/token"
}
