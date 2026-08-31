package bazelcredhelper

import (
	"context"
	"os/exec"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
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
	return func(ctx context.Context) string {
		ctx, cancel := context.WithTimeout(ctx, gitBudget)
		defer cancel()

		return resolveRepoURL(envs, gitCommandFunc(ctx))
	}
}

// resolveRepoURL shares the resolution with the other build tools, then guards
// the value for use as a header.
func resolveRepoURL(envs map[string]string, commandFunc common.CommandFunc) string {
	repoURL, _ := common.ResolveRepoURL(commandFunc, envs)
	if !isHeaderSafe(repoURL) {
		return ""
	}

	return repoURL
}

// gitCommandFunc uses Output() so git's stderr cannot reach stdout, the
// protocol channel.
func gitCommandFunc(ctx context.Context) common.CommandFunc {
	return func(name string, args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, name, args...).Output()

		return string(out), err //nolint:wrapcheck // the caller only branches on nil
	}
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
