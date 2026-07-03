//go:build unit

package common

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

// makeServiceJWT builds a minimal UMA-style Bitrise service JWT carrying org_id,
// so ResolveAuthConfig parses it as a valid JWT credential.
func makeServiceJWT(orgID string) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)

		return base64.RawURLEncoding.EncodeToString(b)
	}
	header := enc(map[string]any{"alg": "none", "typ": "JWT"})
	payload := enc(map[string]any{
		"authorization": map[string]any{
			"permissions": []map[string]any{
				{"rsname": "default", "claims": map[string]any{"org_id": []string{orgID}}},
			},
		},
	})

	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString([]byte("sig"))
}

func TestShadowingAuthEnv(t *testing.T) {
	keyring.MockInit() // empty keychain so a real stored login can't interfere
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "")
	t.Setenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", "")
	if got := shadowingAuthEnv(); got != "" {
		t.Fatalf("expected no shadowing env, got %q", got)
	}

	// The CI service token shadows a stored login.
	t.Setenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", makeServiceJWT("ws-jwt"))
	if got := shadowingAuthEnv(); got != "BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN" {
		t.Fatalf("expected services-token shadow, got %q", got)
	}

	// A manual token + workspace takes precedence over the JWT.
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "tok")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "ws")
	if got := shadowingAuthEnv(); got != "BITRISE_BUILD_CACHE_AUTH_TOKEN" {
		t.Fatalf("expected auth-token shadow, got %q", got)
	}
}
