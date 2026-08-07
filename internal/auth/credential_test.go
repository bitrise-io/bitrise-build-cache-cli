//go:build unit

package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenSetCredential_carriesTheExpiry(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	ts := TokenSet{AuthToken: "pat", WorkspaceID: "ws", Username: "bob", PATExpiry: expiry, RefreshToken: "rt"}

	got := ts.Credential()

	assert.Equal(t, Credential{Token: "pat", WorkspaceID: "ws", Username: "bob", Expiry: expiry}, got)
}

func TestTokenSetOrigin_provenanceFollowsTheRefreshToken(t *testing.T) {
	manual := TokenSet{AuthToken: "pat", WorkspaceID: "ws"}
	login := TokenSet{AuthToken: "pat", WorkspaceID: "ws", RefreshToken: "rt"}

	assert.Equal(t, Origin{Backend: BackendFile, Provenance: ProvenanceManual}, manual.Origin(BackendFile))
	assert.Equal(t, Origin{Backend: BackendKeychain, Provenance: ProvenanceOAuthLogin}, login.Origin(BackendKeychain))
}

func TestOriginStoreManaged(t *testing.T) {
	for backend, want := range map[Backend]bool{
		BackendNone:     false,
		BackendEnv:      false,
		BackendJWT:      false,
		BackendKeychain: true,
		BackendFile:     true,
	} {
		assert.Equal(t, want, Origin{Backend: backend}.StoreManaged(), "backend %d", backend)
	}
}

func TestOriginLabel(t *testing.T) {
	cases := []struct {
		origin           Origin
		label, shortName string
	}{
		{Origin{BackendEnv, ProvenanceInjected}, "environment variables", "env"},
		{Origin{BackendJWT, ProvenanceInjected}, "CI JWT (" + EnvJWT + ")", "jwt"},
		{Origin{BackendKeychain, ProvenanceManual}, "OS keychain", "keychain"},
		{Origin{BackendKeychain, ProvenanceOAuthLogin}, "OAuth login (keychain)", "keychain"},
		{Origin{BackendFile, ProvenanceManual}, "config file (CI-safe)", "config-file"},
		{Origin{BackendFile, ProvenanceOAuthLogin}, "OAuth login (config file)", "config-file"},
		{Origin{BackendFile, ProvenanceLegacy}, "multiplatform config", "multiplatform-config"},
		{Origin{}, "none", "none"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.label, tc.origin.Label())
		assert.Equal(t, tc.shortName, tc.origin.ShortLabel())
	}
}

func TestGradleToken(t *testing.T) {
	cred := Credential{Token: "tok", WorkspaceID: "ws"}

	assert.Equal(t, "tok", GradleToken(cred, Origin{Backend: BackendJWT}))
	assert.Equal(t, "ws:tok", GradleToken(cred, Origin{Backend: BackendKeychain}))
	assert.Equal(t, "tok", GradleToken(Credential{Token: "tok"}, Origin{Backend: BackendEnv}))
}

func TestCredentialExpired(t *testing.T) {
	assert.False(t, Credential{}.Expired(), "zero expiry is unknown, not expired")
	assert.False(t, Credential{Expiry: time.Now().Add(time.Hour)}.Expired())
	assert.True(t, Credential{Expiry: time.Now().Add(-time.Hour)}.Expired())
}

func TestParseJWTWorkspaceID(t *testing.T) {
	// {"authorization":{"permissions":[{"rsname":"default","claims":{"org_id":["ws-1"]}}]}}
	const payload = "eyJhdXRob3JpemF0aW9uIjp7InBlcm1pc3Npb25zIjpbeyJyc25hbWUiOiJkZWZhdWx0IiwiY2xhaW1zIjp7Im9yZ19pZCI6WyJ3cy0xIl19fV19fQ"

	ws, err := ParseJWTWorkspaceID("header." + payload + ".sig")
	require.NoError(t, err)
	assert.Equal(t, "ws-1", ws)

	_, err = ParseJWTWorkspaceID("not-a-jwt")
	assert.Error(t, err)
}
