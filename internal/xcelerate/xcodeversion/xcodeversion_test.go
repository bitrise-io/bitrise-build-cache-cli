//go:build unit

package xcodeversion_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeversion"
)

func TestResolve_ParsesVersionAndBuildNumber(t *testing.T) {
	fake := func(name string, args ...string) (string, error) {
		assert.Equal(t, "/usr/bin/xcodebuild", name)
		assert.Equal(t, []string{"-version"}, args)

		return "Xcode 16.2\nBuild version 16C5032a\n", nil
	}

	version, build, err := xcodeversion.Resolve(t.Context(), "/usr/bin/xcodebuild", fake)
	require.NoError(t, err)
	assert.Equal(t, "16.2", version)
	assert.Equal(t, "16C5032a", build)
}

func TestResolve_CommandFuncError(t *testing.T) {
	fake := func(string, ...string) (string, error) {
		return "", errors.New("exec format error")
	}

	_, _, err := xcodeversion.Resolve(t.Context(), "/usr/bin/xcodebuild", fake)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xcodebuild -version failed")
}

func TestResolve_UnexpectedOutput(t *testing.T) {
	cases := []struct {
		name string
		out  string
	}{
		{name: "single line", out: "Xcode 16.2\n"},
		{name: "missing version prefix", out: "Foo 16.2\nBuild version 16C\n"},
		{name: "missing build prefix", out: "Xcode 16.2\nSomething else\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := func(string, ...string) (string, error) {
				return tc.out, nil
			}

			_, _, err := xcodeversion.Resolve(t.Context(), "/usr/bin/xcodebuild", fake)
			require.Error(t, err)
		})
	}
}

func TestResolve_EmptyPath(t *testing.T) {
	_, _, err := xcodeversion.Resolve(t.Context(), "", func(string, ...string) (string, error) {
		return "", nil
	})
	require.Error(t, err)
}

func TestResolve_NilCommandFunc(t *testing.T) {
	_, _, err := xcodeversion.Resolve(t.Context(), "/usr/bin/xcodebuild", nil)
	require.Error(t, err)
}
