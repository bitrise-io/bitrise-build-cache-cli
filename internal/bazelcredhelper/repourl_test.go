//go:build unit

package bazelcredhelper

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func staticRemote(url string, err error) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		return url, err
	}
}

func TestRepoURLResolver_PrefersTheWorkspaceRemoteOverTheEnv(t *testing.T) {
	resolve := newRepoURLResolver(
		map[string]string{"GIT_REPOSITORY_URL": "https://github.com/org/from-env.git"},
		staticRemote("https://github.com/org/from-git.git\n", nil),
	)

	assert.Equal(t, "https://github.com/org/from-git.git", resolve(t.Context()))
}

func TestRepoURLResolver_FallsBackToEnv_WhenGitIsUnavailable(t *testing.T) {
	resolve := newRepoURLResolver(
		map[string]string{"GIT_REPOSITORY_URL": "https://github.com/org/from-env.git"},
		staticRemote("", errors.New("exec: git: executable file not found in $PATH")),
	)

	assert.Equal(t, "https://github.com/org/from-env.git", resolve(t.Context()))
}

func TestRepoURLResolver_NoGitNoEnv_ReturnsEmpty(t *testing.T) {
	resolve := newRepoURLResolver(map[string]string{}, staticRemote("", errors.New("exit status 1")))

	assert.Empty(t, resolve(t.Context()))
}

func TestRepoURLResolver_RejectsUnsafeHeaderValues(t *testing.T) {
	resolve := newRepoURLResolver(
		map[string]string{"GIT_REPOSITORY_URL": "https://github.com/org/ünicode.git"},
		staticRemote("https://github.com/org/repo.git\ninjected: yes", nil),
	)

	assert.Empty(t, resolve(t.Context()))
}
