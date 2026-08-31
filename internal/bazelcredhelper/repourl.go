package bazelcredhelper

import (
	"context"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// Attributes the invocation to a repository on the Build Cache dashboard.
const repositoryURLHeader = "x-repository-url"

// gitBudget keeps the lookup well inside Budget: attribution must never cost
// the build its credentials.
const gitBudget = 2 * time.Second

// RepoURLResolver returns the repository URL of the workspace being built, or
// "" when there is none to report.
type RepoURLResolver func(ctx context.Context) string

// NewRepoURLResolver resolves the repo URL from the helper's working directory,
// which Bazel sets to the workspace root — so the value follows the project
// being built instead of whichever one `activate bazel` happened to run in.
func NewRepoURLResolver(envs map[string]string) RepoURLResolver {
	return newRepoURLResolver(envs, gitRemoteURL)
}

// Git first, env second, matching common.generateGitMetadata: the env var names
// the repo that triggered the CI build, which is not always the one the Bazel
// workspace belongs to.
func newRepoURLResolver(envs map[string]string, remoteURL func(ctx context.Context) (string, error)) RepoURLResolver {
	return func(ctx context.Context) string {
		ctx, cancel := context.WithTimeout(ctx, gitBudget)
		defer cancel()

		if url, err := remoteURL(ctx); err == nil {
			if url = stripCredentials(strings.TrimSpace(url)); isHeaderSafe(url) {
				return url
			}
		}

		// Covers a workspace outside a git checkout, and containerised builds
		// where git is absent — those get the URL forwarded in the environment.
		if url := stripCredentials(strings.TrimSpace(envs["GIT_REPOSITORY_URL"])); isHeaderSafe(url) {
			return url
		}

		return ""
	}
}

func gitRemoteURL(ctx context.Context) (string, error) {
	// Output() keeps git's stderr away from stdout, the protocol channel.
	out, err := exec.CommandContext(ctx, "git", "config", "--get", "remote.origin.url").Output()

	return string(out), err //nolint:wrapcheck // the caller only branches on nil
}

// stripCredentials drops the userinfo of a URL-form remote. A checkout done by
// a CI provider often leaves a token there (`https://x-access-token:<token>@…`,
// `https://<token>@…`), and this value travels in a header and is persisted.
// scp-form remotes (`git@github.com:org/repo.git`) have no scheme, carry a
// username rather than a secret, and are left alone.
func stripCredentials(rawURL string) string {
	if !strings.Contains(rawURL, "://") {
		return rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User == nil {
		return rawURL
	}

	parsed.User = nil

	return parsed.String()
}

// gRPC rejects metadata outside printable ASCII, which would fail every RPC of
// the build rather than just losing the attribution.
func isHeaderSafe(value string) bool {
	if value == "" {
		return false
	}

	for _, r := range value {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}

	return true
}
