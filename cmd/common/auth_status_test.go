//go:build unit

package common

import (
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/oauth"
)

func TestCurrentAuthStatus(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "")
	t.Setenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", "")

	// Nothing configured.
	if a := currentAuthStatus(); a.Configured || a.Source != "none" {
		t.Fatalf("expected unconfigured, got %+v", a)
	}

	// A stored OAuth login is reported as such, with workspace + a future expiry.
	if err := oauth.Save(oauth.Credentials{
		PAT: "p", RefreshToken: "r", WorkspaceID: "acme",
		PATExpiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := currentAuthStatus()
	if !a.Configured || a.Source != "OAuth login (keychain)" || a.WorkspaceID != "acme" {
		t.Fatalf("expected oauth login, got %+v", a)
	}
	if a.Expired || a.TokenExpiry == "" {
		t.Fatalf("expected a valid (non-expired) token with an expiry, got %+v", a)
	}

	// A manual env token takes precedence over the stored login and is reported
	// as env-sourced (this is also what shadows the login at resolution time).
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "tok")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "ws-env")
	if a := currentAuthStatus(); a.Source != "environment variables" || a.WorkspaceID != "ws-env" {
		t.Fatalf("expected env source winning over stored login, got %+v", a)
	}

	// A malformed service JWT is a real resolution error, surfaced (not "none").
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "")
	t.Setenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", "not-a-jwt")
	if a := currentAuthStatus(); a.Source != "error" || a.Error == "" {
		t.Fatalf("expected surfaced resolution error, got %+v", a)
	}
}
