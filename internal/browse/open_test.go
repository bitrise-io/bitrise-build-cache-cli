//go:build unit

package browse

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLauncherForOS_darwinUsesOpen(t *testing.T) {
	bin, args, ok := launcherForOS("darwin", "https://example.com")
	require.True(t, ok)
	assert.Equal(t, "/usr/bin/open", bin)
	assert.Equal(t, []string{"https://example.com"}, args)
}

func TestLauncherForOS_linuxUsesXdgOpen(t *testing.T) {
	bin, args, ok := launcherForOS("linux", "https://example.com")
	require.True(t, ok)
	assert.Equal(t, "xdg-open", bin)
	assert.Equal(t, []string{"https://example.com"}, args)
}

func TestLauncherForOS_unknownPlatformReturnsNotOk(t *testing.T) {
	_, _, ok := launcherForOS("windows", "https://example.com")
	assert.False(t, ok, "windows + unknown platforms should fall back to print-URL")
}

func TestDefaultOpener_invokesRunnerWithLauncher(t *testing.T) {
	var seenBin string
	var seenArgs []string

	opener := DefaultOpener{
		CommandRunner: func(_ context.Context, bin string, args ...string) error {
			seenBin = bin
			seenArgs = append([]string(nil), args...)

			return nil
		},
	}

	err := opener.Open(context.Background(), "https://example.com")
	require.NoError(t, err)

	assert.NotEmpty(t, seenBin)
	require.Len(t, seenArgs, 1)
	assert.Equal(t, "https://example.com", seenArgs[0])
}

func TestDefaultOpener_runnerErrorPropagates(t *testing.T) {
	opener := DefaultOpener{
		CommandRunner: func(context.Context, string, ...string) error {
			return errors.New("simulated exec failure")
		},
	}

	err := opener.Open(context.Background(), "https://example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated exec failure")
}
