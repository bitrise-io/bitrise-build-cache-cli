package common

import (
	"net/url"
	"strings"
)

// ResolveRepoURL returns the repository URL of commandFunc's working directory,
// falling back to GIT_REPOSITORY_URL. The returned error is the git failure, for
// callers that log it; the URL is resolved either way.
func ResolveRepoURL(commandFunc CommandFunc, envs map[string]string) (string, error) {
	repoURL, err := commandFunc("git", "config", "--get", "remote.origin.url")
	if err != nil || strings.TrimSpace(repoURL) == "" {
		return stripRepoURLCredentials(strings.TrimSpace(envs["GIT_REPOSITORY_URL"])), err
	}

	return stripRepoURLCredentials(strings.TrimSpace(repoURL)), nil
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
