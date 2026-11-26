// Package testhome isolates a test process from the developer's home dir, keychain, credentials and CI env.
package testhome

import (
	"os"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

// A re-exec'd child of a prepared test process inherits this and keeps the HOME its parent gave it.
const envMarker = "BITRISE_BUILD_CACHE_TESTHOME"

func Main(m *testing.M) {
	cleanup := Setup()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

// Setup is Main for packages with their own TestMain; run the returned cleanup after m.Run.
func Setup() func() {
	keyring.MockInit()

	// Only a live parent's dir counts, so a marker leaked into a shell cannot switch isolation off.
	if dir := os.Getenv(envMarker); dir != "" {
		if _, err := os.Stat(dir); err == nil {
			return func() {}
		}
	}

	for _, k := range []string{
		auth.EnvAuthToken,
		auth.EnvWorkspaceID,
		auth.EnvJWT,
		auth.EnvUsername,
		auth.EnvBuildHubVMToken,
		auth.EnvBuildHubVMTokenURL,
		auth.EnvOIDCPolicyID,
		auth.EnvGitHubOIDCRequestURL,
		auth.EnvGitHubOIDCRequestToken,
		"CI",
		"CIRCLECI",
		"GITHUB_ACTIONS",
		"GITLAB_CI",
		"BITRISE_IO",
		"BITRISE_BUILD_SLUG",
	} {
		mustDo(os.Unsetenv(k))
	}

	home, err := os.MkdirTemp("", "bbc-testhome-")
	mustDo(err)
	mustDo(os.Setenv("HOME", home))
	mustDo(os.Setenv(envMarker, home))

	return func() { _ = os.RemoveAll(home) }
}

func mustDo(err error) {
	if err != nil {
		panic(err)
	}
}
