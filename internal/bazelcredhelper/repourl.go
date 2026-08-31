package bazelcredhelper

import (
	"context"
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

func newRepoURLResolver(envs map[string]string, remoteURL func(ctx context.Context) (string, error)) RepoURLResolver {
	return func(ctx context.Context) string {
		if url := strings.TrimSpace(envs["GIT_REPOSITORY_URL"]); isHeaderSafe(url) {
			return url
		}

		ctx, cancel := context.WithTimeout(ctx, gitBudget)
		defer cancel()

		url, err := remoteURL(ctx)
		if err != nil {
			return ""
		}

		if url = strings.TrimSpace(url); !isHeaderSafe(url) {
			return ""
		}

		return url
	}
}

func gitRemoteURL(ctx context.Context) (string, error) {
	// Output() keeps git's stderr away from stdout, the protocol channel.
	out, err := exec.CommandContext(ctx, "git", "config", "--get", "remote.origin.url").Output()

	return string(out), err //nolint:wrapcheck // the caller only branches on nil
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
