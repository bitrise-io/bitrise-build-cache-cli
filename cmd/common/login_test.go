//go:build unit

package common

import "testing"

func TestShadowingAuthEnv(t *testing.T) {
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "")
	t.Setenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", "")
	if got := shadowingAuthEnv(); got != "" {
		t.Fatalf("expected no shadowing env, got %q", got)
	}

	// The CI service token shadows a stored login.
	t.Setenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", "jwt")
	if got := shadowingAuthEnv(); got != "BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN" {
		t.Fatalf("expected services-token shadow, got %q", got)
	}

	// The manual auth token is reported first when both are set.
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "tok")
	if got := shadowingAuthEnv(); got != "BITRISE_BUILD_CACHE_AUTH_TOKEN" {
		t.Fatalf("expected auth-token shadow, got %q", got)
	}
}
