//go:build unit

package devdir

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func TestFromXcodebuildPath(t *testing.T) {
	cases := map[string]string{
		"/Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild": "/Applications/Xcode.app/Contents/Developer",
		"/opt/some-wrapper/xcodebuild": "",
		"":                             "",
	}
	for in, want := range cases {
		assert.Equal(t, want, FromXcodebuildPath(in), "input: %s", in)
	}
}

func TestResolve_derivesFromXcodebuildPath(t *testing.T) {
	cmdFunc := func(_ context.Context, cmd string, args ...string) utils.Command {
		t.Fatalf("cmd should not be invoked (got %s %v)", cmd, args)

		return nil
	}

	got, err := Resolve(context.Background(), cmdFunc,
		"/Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild")
	require.NoError(t, err)
	assert.Equal(t, "/Applications/Xcode.app/Contents/Developer", got)
}

func TestResolve_fallsBackToXcodeSelect(t *testing.T) {
	called := false
	cmdFunc := func(_ context.Context, cmd string, args ...string) utils.Command {
		if cmd == "xcode-select" && len(args) > 0 && args[0] == "-p" {
			called = true

			return &utilsMocks.CommandMock{
				CombinedOutputFunc: func() ([]byte, error) {
					return []byte("/Applications/Xcode.app/Contents/Developer\n"), nil
				},
			}
		}

		t.Fatalf("unexpected command %s %v", cmd, args)

		return nil
	}

	got, err := Resolve(context.Background(), cmdFunc, "/opt/some-wrapper/xcodebuild")
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "/Applications/Xcode.app/Contents/Developer", got)
}

func TestResolve_xcodeSelectErrorPropagates(t *testing.T) {
	cmdFunc := func(_ context.Context, _ string, _ ...string) utils.Command {
		return &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return nil, errors.New("xcode-select missing")
			},
		}
	}

	_, err := Resolve(context.Background(), cmdFunc, "/opt/not-matching/path")
	require.Error(t, err)
}

func TestResolve_xcodeSelectEmptyIsError(t *testing.T) {
	cmdFunc := func(_ context.Context, _ string, _ ...string) utils.Command {
		return &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("\n"), nil
			},
		}
	}

	_, err := Resolve(context.Background(), cmdFunc, "/opt/not-matching/path")
	require.Error(t, err)
}
