//go:build unit

package reactnative

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsEASBuildInvocation(t *testing.T) {
	cases := []struct {
		name    string
		binary  string
		args    []string
		want    bool
	}{
		{"direct eas build", "eas", []string{"build", "--platform=ios", "--local"}, true},
		{"eas with no subcommand", "eas", []string{}, false},
		{"eas with non-build subcommand", "eas", []string{"submit"}, false},
		{"npx eas build", "npx", []string{"eas", "build", "--local"}, true},
		{"pnpm eas build", "pnpm", []string{"eas", "build"}, true},
		{"yarn eas build", "yarn", []string{"eas", "build"}, true},
		{"bunx eas build", "bunx", []string{"eas", "build"}, true},
		{"bun eas build", "bun", []string{"eas", "build"}, true},
		{"npm eas build is not detected", "npm", []string{"eas", "build"}, false},
		{"npx eas submit is not detected", "npx", []string{"eas", "submit"}, false},
		{"unrelated binary", "xcodebuild", []string{"build"}, false},
		{"yarn build alone", "yarn", []string{"build"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsEASBuildInvocation(tc.binary, tc.args))
		})
	}
}

func TestDefaultEASWorkingDir(t *testing.T) {
	t.Run("Bitrise CI gets the documented /Users/vagrant/build path", func(t *testing.T) {
		envs := map[string]string{
			"BITRISE_IO":         "true",
			"BITRISE_BUILD_SLUG": "abc",
			"HOME":               "/Users/someone-else",
		}
		assert.Equal(t, "/Users/vagrant/build", DefaultEASWorkingDir(envs))
	})

	t.Run("GitHub Actions falls back to $HOME/build", func(t *testing.T) {
		envs := map[string]string{
			"GITHUB_ACTIONS": "true",
			"HOME":           "/Users/runner",
		}
		assert.Equal(t, "/Users/runner/build", DefaultEASWorkingDir(envs))
	})

	t.Run("local (no CI) falls back to $HOME/build", func(t *testing.T) {
		envs := map[string]string{
			"HOME": "/Users/dev",
		}
		assert.Equal(t, "/Users/dev/build", DefaultEASWorkingDir(envs))
	})

	t.Run("HOME missing falls back to bitrise default", func(t *testing.T) {
		assert.Equal(t, "/Users/vagrant/build", DefaultEASWorkingDir(map[string]string{}))
	})
}

func TestEnvironContains(t *testing.T) {
	environ := []string{"FOO=bar", "BAZ=", "EAS_LOCAL_BUILD_WORKINGDIR=/tmp/x"}
	assert.True(t, environContains(environ, "FOO"))
	assert.True(t, environContains(environ, "BAZ"))
	assert.True(t, environContains(environ, "EAS_LOCAL_BUILD_WORKINGDIR"))
	assert.False(t, environContains(environ, "MISSING"))
	assert.False(t, environContains(environ, "FOO=bar")) // not a prefix match
}

func TestEnvironToMap(t *testing.T) {
	got := environToMap([]string{"A=1", "B=two=halves", "MALFORMED", "=skip-empty-key"})
	assert.Equal(t, "1", got["A"])
	assert.Equal(t, "two=halves", got["B"])
	_, hasMalformed := got["MALFORMED"]
	assert.False(t, hasMalformed)
	_, hasEmpty := got[""]
	assert.False(t, hasEmpty)
}
