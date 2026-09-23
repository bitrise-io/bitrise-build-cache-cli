//go:build unit

package bazelcredhelper

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func staticRemote(url string, err error) func(string, ...string) (string, error) {
	return func(string, ...string) (string, error) {
		return url, err
	}
}

func TestResolveRepoURL_PrefersTheWorkspaceRemoteOverTheEnv(t *testing.T) {
	got := resolveRepoURL(
		map[string]string{"GIT_REPOSITORY_URL": "https://github.com/org/from-env.git"},
		staticRemote("https://github.com/org/from-git.git\n", nil),
	)

	assert.Equal(t, "https://github.com/org/from-git.git", got)
}

func TestResolveRepoURL_FallsBackToEnv_WhenGitIsUnavailable(t *testing.T) {
	got := resolveRepoURL(
		map[string]string{"GIT_REPOSITORY_URL": "https://github.com/org/from-env.git"},
		staticRemote("", errors.New("exec: git: executable file not found in $PATH")),
	)

	assert.Equal(t, "https://github.com/org/from-env.git", got)
}

func TestResolveRepoURL_NoGitNoEnv_ReturnsEmpty(t *testing.T) {
	got := resolveRepoURL(map[string]string{}, staticRemote("", errors.New("exit status 1")))

	assert.Empty(t, got)
}

func TestResolveRepoURL_RejectsValuesUnusableAsAHeader(t *testing.T) {
	got := resolveRepoURL(
		map[string]string{"GIT_REPOSITORY_URL": "https://github.com/org/ünicode.git"},
		staticRemote("https://github.com/org/repo.git\ninjected: yes", nil),
	)

	assert.Empty(t, got)
}

func TestResolveRepoURL_StripsCredentials(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote string
		want   string
	}{
		{
			name:   "token as user and password",
			remote: "https://x-access-token:ghs_secretsecret@github.com/org/repo.git",
			want:   "https://github.com/org/repo.git",
		},
		{
			name:   "token as user only",
			remote: "https://ghp_secretsecret@github.com/org/repo.git",
			want:   "https://github.com/org/repo.git",
		},
		{
			name:   "scp form is left alone",
			remote: "git@github.com:org/repo.git",
			want:   "git@github.com:org/repo.git",
		},
		{
			name:   "plain https is left alone",
			remote: "https://github.com/org/repo.git",
			want:   "https://github.com/org/repo.git",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveRepoURL(map[string]string{}, staticRemote(tc.remote, nil)))
		})
	}
}
