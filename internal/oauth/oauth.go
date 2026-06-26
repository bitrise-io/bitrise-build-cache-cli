// Package oauth implements the browser OAuth login (authorization-code + PKCE
// over a loopback redirect) that mints a Bitrise PAT, the transparent PAT
// refresh, and the on-disk credential store. Ported from bitrise-cli; none of
// the identity inputs are secret.
package oauth

import (
	"net/http"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

// Identity defaults (production), each overridable per environment via the env
// vars in NewConfigFromEnv; none is a secret.
const (
	DefaultIssuer       = "https://oauth.bitrise.io"
	DefaultClientID     = "https://app.bitrise.io/.well-known/oauth-client/cli"
	DefaultOIDCEndpoint = "https://app.bitrise.io/oidc/token"
	// Must be registered as a WorkOS Resource Indicator; within the monolith's
	// *.bitrise.io audience allowlist.
	DefaultResource = "https://app.bitrise.io"
)

// refreshSkew re-mints a PAT slightly before expiry; defaultPATLifetime is the
// fallback when the exchange omits expires_in.
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
	Logger            log.Logger // optional; nil disables logging
}

func (c Config) debugf(format string, args ...any) { //nolint:unparam // variadic for symmetry with infof/warnf and future callers
	if c.Logger != nil {
		c.Logger.Debugf(format, args...)
	}
}

func (c Config) infof(format string, args ...any) {
	if c.Logger != nil {
		c.Logger.Infof(format, args...)
	}
}

func (c Config) warnf(format string, args ...any) {
	if c.Logger != nil {
		c.Logger.Warnf(format, args...)
	}
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
