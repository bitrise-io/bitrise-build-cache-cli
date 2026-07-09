//go:build unit

package xcode_app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	calls  []runnerCall
	stdout string
	stderr string
	exit   int
	err    error
}

type runnerCall struct {
	bin  string
	args []string
}

func (f *fakeRunner) Run(_ context.Context, bin string, args ...string) (string, string, int, error) {
	f.calls = append(f.calls, runnerCall{bin: bin, args: append([]string(nil), args...)})

	return f.stdout, f.stderr, f.exit, f.err
}

func TestLaunchctl_Setenv_invokesLaunchctlWithKeyValue(t *testing.T) {
	r := &fakeRunner{}
	c := LaunchctlClient{Runner: r, Bin: "/fake/launchctl"}

	require.NoError(t, c.Setenv(context.Background(), XCConfigEnvVar, "/path/x.xcconfig"))

	require.Len(t, r.calls, 1)
	assert.Equal(t, "/fake/launchctl", r.calls[0].bin)
	assert.Equal(t, []string{"setenv", XCConfigEnvVar, "/path/x.xcconfig"}, r.calls[0].args)
}

func TestLaunchctl_Setenv_nonZeroExitIsError(t *testing.T) {
	r := &fakeRunner{exit: 1, stderr: "boom"}
	c := LaunchctlClient{Runner: r}

	err := c.Setenv(context.Background(), "K", "v")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestLaunchctl_Unsetenv_exit113IsSuccess(t *testing.T) {
	r := &fakeRunner{exit: 113}
	c := LaunchctlClient{Runner: r}

	require.NoError(t, c.Unsetenv(context.Background(), "K"))
}

func TestLaunchctl_Unsetenv_otherNonZeroIsError(t *testing.T) {
	r := &fakeRunner{exit: 1, stderr: "permission denied"}
	c := LaunchctlClient{Runner: r}

	err := c.Unsetenv(context.Background(), "K")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestLaunchctl_Bootstrap_runsBootoutThenBootstrap(t *testing.T) {
	r := &fakeRunner{}
	c := LaunchctlClient{Runner: r}

	require.NoError(t, c.Bootstrap(context.Background(), "/plist"))

	require.Len(t, r.calls, 2)
	assert.Equal(t, "bootout", r.calls[0].args[0])
	assert.Equal(t, "bootstrap", r.calls[1].args[0])
}

func TestLaunchctl_Bootout_notLoadedTreatedAsSuccess(t *testing.T) {
	r := &fakeRunner{exit: 113, stderr: "Could not find service \"foo\" in domain for gui/501"}
	c := LaunchctlClient{Runner: r}

	require.NoError(t, c.Bootout(context.Background(), "/plist"))
}

func TestLaunchctl_Bootout_realErrorPropagates(t *testing.T) {
	r := &fakeRunner{exit: 1, stderr: "permission denied"}
	c := LaunchctlClient{Runner: r}

	err := c.Bootout(context.Background(), "/plist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestLaunchctl_runnerErrorPropagates(t *testing.T) {
	r := &fakeRunner{err: errors.New("exec failed")}
	c := LaunchctlClient{Runner: r}

	err := c.Setenv(context.Background(), "K", "v")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exec failed")
}
