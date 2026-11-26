// Package testhome isolates a test process from the developer's home and temp dirs, keychain, credentials and CI env.
package testhome

import (
	"os"
	"path/filepath"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

// A re-exec'd child of a prepared test process inherits this and keeps the HOME and TMPDIR its parent gave it.
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

	// A marker whose dir is gone cannot switch isolation off.
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
		"GITHUB_ENV",
		"GITHUB_PATH",
		"GITHUB_STEP_SUMMARY",
		// Literals: the packages that own these import testhome in their tests.
		"BITRISE_XCELERATE_PROXY_SOCKET_PATH",
		"BITRISE_CCACHE_IPC_SOCKET_PATH",
	} {
		mustDo(os.Unsetenv(k))
	}

	home, err := os.MkdirTemp("", "bbc-testhome-")
	mustDo(err)
	// Created before TMPDIR moves, so it lands in the real temp dir.
	tmp, err := os.MkdirTemp("", "bbc-tmp-")
	mustDo(err)

	mustDo(os.Setenv("HOME", home))
	mustDo(os.Setenv("TMPDIR", tmp))
	// envman otherwise writes to the CI step's envstore, or to ./.envstore.yml.
	mustDo(os.Setenv("ENVMAN_ENVSTORE_PATH", filepath.Join(home, ".envstore.yml")))
	mustDo(os.Setenv(envMarker, home))

	return func() {
		_ = os.RemoveAll(home)
		_ = os.RemoveAll(tmp)
	}
}

func mustDo(err error) {
	if err != nil {
		panic(err)
	}
}
