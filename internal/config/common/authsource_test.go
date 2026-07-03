//go:build unit

package common

import (
	"strings"
	"testing"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
)

func TestSourceLabel(t *testing.T) {
	cases := []struct {
		source     AuthSource
		oauthLogin bool
		want       string
	}{
		{AuthSourceEnvVars, false, "environment variables"},
		{AuthSourceJWT, false, "CI JWT (" + EnvJWT + ")"},
		{AuthSourceKeychain, false, "OS keychain"},
		{AuthSourceKeychain, true, "OAuth login (keychain)"},
		{AuthSourceMultiplatform, false, "multiplatform config"},
		{AuthSourceNone, false, "none"},
	}
	for _, tc := range cases {
		if got := SourceLabel(tc.source, tc.oauthLogin); got != tc.want {
			t.Errorf("SourceLabel(%v,%v) = %q, want %q", tc.source, tc.oauthLogin, got, tc.want)
		}
	}
}

func TestDescribeResolvedWith_keychainOAuthLogin(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	loader := fakeAuthLoader{creds: keychain.Credentials{
		AuthToken: "p", WorkspaceID: "acme", RefreshToken: "r", PATExpiry: exp,
	}}

	d := DescribeResolvedWith(CacheAuthConfig{AuthToken: "p", WorkspaceID: "acme"}, AuthSourceKeychain, loader)
	if !d.IsOAuthLogin || d.WorkspaceID != "acme" || !d.PATExpiry.Equal(exp) {
		t.Fatalf("oauth keychain: got %+v", d)
	}
	if d.Label() != "OAuth login (keychain)" {
		t.Fatalf("label = %q", d.Label())
	}
	if d.Expired() {
		t.Fatal("future expiry should not be expired")
	}
	if !strings.Contains(d.Detail(), "workspace acme") || !strings.Contains(d.Detail(), "token valid until") {
		t.Fatalf("detail = %q", d.Detail())
	}
}

func TestDescribeResolvedWith_keychainManual(t *testing.T) {
	loader := fakeAuthLoader{creds: keychain.Credentials{AuthToken: "p", WorkspaceID: "acme"}}

	d := DescribeResolvedWith(CacheAuthConfig{AuthToken: "p", WorkspaceID: "acme"}, AuthSourceKeychain, loader)
	if d.IsOAuthLogin || d.Label() != "OS keychain" {
		t.Fatalf("manual keychain: got %+v label %q", d, d.Label())
	}
	if !d.PATExpiry.IsZero() {
		t.Fatalf("manual keychain should have no expiry, got %v", d.PATExpiry)
	}
}

func TestDescribeResolved_jwtHidesWorkspace(t *testing.T) {
	d := DescribeResolved(CacheAuthConfig{AuthToken: "jwt", WorkspaceID: "from-jwt"}, AuthSourceJWT)
	if d.WorkspaceID != "" {
		t.Fatalf("JWT workspace should not be surfaced, got %q", d.WorkspaceID)
	}
	if d.Label() != "CI JWT ("+EnvJWT+")" {
		t.Fatalf("label = %q", d.Label())
	}
}

func TestDescribeResolved_expiredPAT(t *testing.T) {
	d := AuthDescription{Source: AuthSourceKeychain, IsOAuthLogin: true, PATExpiry: time.Now().Add(-time.Minute)}
	if !d.Expired() {
		t.Fatal("past expiry should be expired")
	}
	if !strings.Contains(d.Detail(), "token expired") {
		t.Fatalf("detail = %q", d.Detail())
	}
}
