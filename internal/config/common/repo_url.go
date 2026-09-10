package common

import (
	"net/url"
	"strings"
)

// ResolveRepoURL returns the repository URL of commandFunc's working directory,
// falling back to GIT_REPOSITORY_URL when git reports no usable remote. The
// returned error is the git failure, for callers that log it; the URL is
// resolved either way.
func ResolveRepoURL(commandFunc CommandFunc, envs map[string]string) (string, error) {
	remote, err := commandFunc("git", "config", "--get", "remote.origin.url")
	if repoURL := strings.TrimSpace(remote); err == nil && isRemoteRepoURL(repoURL) {
		return stripRepoURLCredentials(repoURL), nil
	}

	if envURL := strings.TrimSpace(envs["GIT_REPOSITORY_URL"]); isRemoteRepoURL(envURL) {
		return stripRepoURLCredentials(envURL), err
	}

	return "", err
}

// isRemoteRepoURL reports whether the value identifies a repository outside the
// machine it was read on. git also accepts local paths as remotes, and a CI
// setup that rebuilds the repo from a tarball (`git init` + `git remote add
// origin .`) would otherwise report `.` as the project on the dashboard.
func isRemoteRepoURL(repoURL string) bool {
	if scheme, rest, found := strings.Cut(repoURL, "://"); found {
		return rest != "" && !strings.EqualFold(scheme, "file")
	}

	// scp-form (`git@github.com:org/repo.git`, `github.com:org/repo.git`) is the
	// only remaining remote form; anything else is a path.
	host, path, found := strings.Cut(repoURL, ":")
	if !found || path == "" || strings.Contains(host, "/") {
		return false
	}

	return strings.Contains(host, "@") || strings.Contains(host, ".")
}

// stripRepoURLCredentials drops the userinfo of a URL-form remote. A checkout
// done by a CI provider often leaves a token there (`https://x-access-token:
// <token>@…`, `https://<token>@…`), and this value is sent as a header and
// persisted with the invocation. scp-form remotes (`git@github.com:org/repo.git`)
// have no scheme, carry a username rather than a secret, and are left alone.
func stripRepoURLCredentials(rawURL string) string {
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
