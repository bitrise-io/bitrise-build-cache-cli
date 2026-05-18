//go:build unit

package reactnative

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRunner(params RunnerParams) *Runner {
	r := NewRunner(params)
	r.socket = nil
	r.postRun = nil

	return r
}

// activateRNHome points HOME at a temp dir and writes the marker + multiplatform
// configs the Runner needs to consider RN "activated" (so the run path is not
// bypassed by isReactNativeReady). Returns the fake HOME.
func activateRNHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	rnDir := filepath.Join(home, ".bitrise/cache/reactnative")
	require.NoError(t, os.MkdirAll(rnDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rnDir, "config.json"), []byte(`{"enabled":true}`), 0o644))

	mpDir := filepath.Join(home, ".bitrise/analytics/multiplatform")
	require.NoError(t, os.MkdirAll(mpDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(mpDir, "config.json"),
		[]byte(`{"authConfig":{"AuthToken":"tok","WorkspaceID":"ws-slug","IsJWT":false}}`),
		0o644,
	))

	return home
}

func TestRunner_Run(t *testing.T) {
	activateRNHome(t)
	ctx := context.Background()

	t.Run("args are passed through to the command", func(t *testing.T) {
		var capturedName string
		var capturedArgs []string

		r := newTestRunner(RunnerParams{
			ExecFn: func(_ []string, name string, args ...string) error {
				capturedName = name
				capturedArgs = args

				return nil
			},
		})
		err := r.Run(ctx, []string{"bash", "-c", "echo hello"}, "", []string{})

		require.NoError(t, err)
		assert.Equal(t, "bash", capturedName)
		assert.Equal(t, []string{"-c", "echo hello"}, capturedArgs)
	})

	t.Run("BITRISE_INVOCATION_ID is injected into the environment", func(t *testing.T) {
		var capturedEnviron []string

		r := newTestRunner(RunnerParams{
			ExecFn: func(environ []string, _ string, _ ...string) error {
				capturedEnviron = environ

				return nil
			},
		})
		err := r.Run(ctx, []string{"true"}, "", []string{"EXISTING=value"})

		require.NoError(t, err)
		assert.Contains(t, capturedEnviron, "EXISTING=value")

		var invocationIDEntry string
		for _, e := range capturedEnviron {
			if strings.HasPrefix(e, "BITRISE_INVOCATION_ID=") {
				invocationIDEntry = e

				break
			}
		}
		assert.NotEmpty(t, invocationIDEntry)
		assert.NotEqual(t, "BITRISE_INVOCATION_ID=", invocationIDEntry)
	})

	t.Run("each call generates a distinct invocation ID", func(t *testing.T) {
		extractID := func() string {
			var id string
			r := newTestRunner(RunnerParams{
				ExecFn: func(environ []string, _ string, _ ...string) error {
					for _, e := range environ {
						if strings.HasPrefix(e, "BITRISE_INVOCATION_ID=") {
							id = strings.TrimPrefix(e, "BITRISE_INVOCATION_ID=")
						}
					}

					return nil
				},
			})
			_ = r.Run(ctx, []string{"true"}, "", []string{})

			return id
		}

		id1 := extractID()
		id2 := extractID()
		assert.NotEmpty(t, id1)
		assert.NotEmpty(t, id2)
		assert.NotEqual(t, id1, id2)
	})

	t.Run("error from execFn is propagated", func(t *testing.T) {
		execErr := errors.New("exec failed")
		r := newTestRunner(RunnerParams{
			ExecFn: func(_ []string, _ string, _ ...string) error { return execErr },
		})

		err := r.Run(ctx, []string{"true"}, "", []string{})
		assert.ErrorIs(t, err, execErr)
	})

	t.Run("missing args returns error", func(t *testing.T) {
		r := newTestRunner(RunnerParams{
			ExecFn: func(_ []string, _ string, _ ...string) error { return nil },
		})

		err := r.Run(ctx, []string{}, "", []string{})
		assert.Error(t, err)
	})

	t.Run("provided invocation ID is used as-is", func(t *testing.T) {
		const fixedID = "fixed-invocation-id"
		var capturedEnvID string

		r := newTestRunner(RunnerParams{
			ExecFn: func(environ []string, _ string, _ ...string) error {
				for _, e := range environ {
					if strings.HasPrefix(e, "BITRISE_INVOCATION_ID=") {
						capturedEnvID = strings.TrimPrefix(e, "BITRISE_INVOCATION_ID=")
					}
				}

				return nil
			},
		})
		err := r.Run(ctx, []string{"true"}, fixedID, []string{})

		require.NoError(t, err)
		assert.Equal(t, fixedID, capturedEnvID)
	})
}

func TestRunner_PostRunHook(t *testing.T) {
	activateRNHome(t)
	ctx := context.Background()
	noOpExec := func(_ []string, _ string, _ ...string) error { return nil }

	t.Run("calls postRun with invocation ID and args", func(t *testing.T) {
		mock := &postRunRunnerMock{}
		r := newTestRunner(RunnerParams{ExecFn: noOpExec})
		r.postRun = mock

		err := r.Run(ctx, []string{"yarn", "build"}, "inv-123", []string{})

		require.NoError(t, err)
		require.Len(t, mock.runCalls(), 1)

		call := mock.runCalls()[0]
		assert.Equal(t, "inv-123", call.WrapperInvocationID)
		assert.Equal(t, []string{"yarn", "build"}, call.Args)
		assert.Nil(t, call.ExecErr)
	})

	t.Run("passes exec error to postRun", func(t *testing.T) {
		execErr := errors.New("build failed")
		mock := &postRunRunnerMock{}
		r := newTestRunner(RunnerParams{
			ExecFn: func(_ []string, _ string, _ ...string) error { return execErr },
		})
		r.postRun = mock

		_ = r.Run(ctx, []string{"yarn", "build"}, "inv-456", []string{})

		require.Len(t, mock.runCalls(), 1)
		assert.ErrorIs(t, mock.runCalls()[0].ExecErr, execErr)
	})

	t.Run("generates invocation ID when empty and passes it to postRun", func(t *testing.T) {
		mock := &postRunRunnerMock{}
		r := newTestRunner(RunnerParams{ExecFn: noOpExec})
		r.postRun = mock

		err := r.Run(ctx, []string{"true"}, "", []string{})

		require.NoError(t, err)
		require.Len(t, mock.runCalls(), 1)
		assert.NotEmpty(t, mock.runCalls()[0].WrapperInvocationID)
	})

	t.Run("postRun is not called when postRun is nil", func(t *testing.T) {
		r := newTestRunner(RunnerParams{ExecFn: noOpExec})
		// postRun is already nil from newTestRunner

		err := r.Run(ctx, []string{"true"}, "inv", []string{})

		require.NoError(t, err)
		// no panic — nil postRun is handled gracefully
	})

	t.Run("postRun is called even when exec fails", func(t *testing.T) {
		mock := &postRunRunnerMock{}
		r := newTestRunner(RunnerParams{
			ExecFn: func(_ []string, _ string, _ ...string) error { return errors.New("failed") },
		})
		r.postRun = mock

		_ = r.Run(ctx, []string{"true"}, "inv", []string{})

		require.Len(t, mock.runCalls(), 1)
	})
}

func TestRunner_BypassWhenNotActivated(t *testing.T) {
	ctx := context.Background()

	t.Run("missing RN marker → command runs unwrapped, no invocation ID injected, postRun not called", func(t *testing.T) {
		// Empty HOME — no RN marker, no multiplatform config.
		t.Setenv("HOME", t.TempDir())

		var capturedName string
		var capturedArgs []string
		var capturedEnviron []string

		mock := &postRunRunnerMock{}
		r := NewRunner(RunnerParams{
			ExecFn: func(environ []string, name string, args ...string) error {
				capturedName = name
				capturedArgs = args
				capturedEnviron = environ

				return nil
			},
		})
		r.socket = nil
		r.postRun = mock

		err := r.Run(ctx, []string{"yarn", "build"}, "given-id", []string{"EXISTING=value"})

		require.NoError(t, err)
		assert.Equal(t, "yarn", capturedName)
		assert.Equal(t, []string{"build"}, capturedArgs)
		assert.Equal(t, []string{"EXISTING=value"}, capturedEnviron, "environ must be unchanged when bypassed")
		for _, e := range capturedEnviron {
			assert.False(t, strings.HasPrefix(e, "BITRISE_INVOCATION_ID="), "no invocation ID should be injected when bypassed")
		}
		assert.Empty(t, mock.runCalls(), "postRun must not fire when bypassed")
	})

	t.Run("missing workspace ID → command runs unwrapped", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		// RN marker present but multiplatform config has empty WorkspaceID.
		rnDir := filepath.Join(home, ".bitrise/cache/reactnative")
		require.NoError(t, os.MkdirAll(rnDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(rnDir, "config.json"), []byte(`{"enabled":true}`), 0o644))

		mpDir := filepath.Join(home, ".bitrise/analytics/multiplatform")
		require.NoError(t, os.MkdirAll(mpDir, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(mpDir, "config.json"),
			[]byte(`{"authConfig":{"AuthToken":"tok","WorkspaceID":"","IsJWT":false}}`),
			0o644,
		))

		var capturedEnviron []string
		mock := &postRunRunnerMock{}
		r := NewRunner(RunnerParams{
			ExecFn: func(environ []string, _ string, _ ...string) error {
				capturedEnviron = environ

				return nil
			},
		})
		r.socket = nil
		r.postRun = mock

		err := r.Run(ctx, []string{"yarn", "build"}, "given-id", []string{"EXISTING=value"})

		require.NoError(t, err)
		for _, e := range capturedEnviron {
			assert.False(t, strings.HasPrefix(e, "BITRISE_INVOCATION_ID="), "no invocation ID should be injected when bypassed")
		}
		assert.Empty(t, mock.runCalls(), "postRun must not fire when bypassed")
	})
}

func TestRunner_Run_EASWorkingDir(t *testing.T) {
	activateRNHome(t)
	ctx := context.Background()

	captureEnv := func(t *testing.T, args []string, environ []string) []string {
		t.Helper()

		var captured []string
		r := newTestRunner(RunnerParams{
			ExecFn: func(env []string, _ string, _ ...string) error {
				captured = env

				return nil
			},
		})
		require.NoError(t, r.Run(ctx, args, "", environ))

		return captured
	}

	findEnv := func(environ []string, key string) (string, bool) {
		prefix := key + "="
		for _, e := range environ {
			if strings.HasPrefix(e, prefix) {
				return strings.TrimPrefix(e, prefix), true
			}
		}

		return "", false
	}

	t.Run("eas build → workdir injected", func(t *testing.T) {
		got := captureEnv(t, []string{"eas", "build", "--platform=ios", "--local"}, []string{"HOME=/Users/dev"})
		val, ok := findEnv(got, EASWorkingDirEnv)
		require.True(t, ok, "EAS workdir env should be injected")
		assert.Equal(t, "/Users/dev/build", val)
	})

	t.Run("npx eas build → workdir injected", func(t *testing.T) {
		got := captureEnv(t, []string{"npx", "eas", "build"}, []string{"HOME=/Users/dev"})
		_, ok := findEnv(got, EASWorkingDirEnv)
		assert.True(t, ok)
	})

	t.Run("non-eas command → workdir NOT injected", func(t *testing.T) {
		got := captureEnv(t, []string{"yarn", "build"}, []string{"HOME=/Users/dev"})
		_, ok := findEnv(got, EASWorkingDirEnv)
		assert.False(t, ok)
	})

	t.Run("user-supplied workdir is preserved", func(t *testing.T) {
		got := captureEnv(t,
			[]string{"eas", "build"},
			[]string{"HOME=/Users/dev", EASWorkingDirEnv + "=/custom/path"},
		)
		val, _ := findEnv(got, EASWorkingDirEnv)
		assert.Equal(t, "/custom/path", val)
	})

	t.Run("HOME=/Users/vagrant (Bitrise stack default) → /Users/vagrant/build", func(t *testing.T) {
		got := captureEnv(t,
			[]string{"eas", "build"},
			[]string{"HOME=/Users/vagrant", "BITRISE_IO=true", "BITRISE_BUILD_SLUG=abc"},
		)
		val, _ := findEnv(got, EASWorkingDirEnv)
		assert.Equal(t, "/Users/vagrant/build", val)
	})

	t.Run("HOME missing → no injection", func(t *testing.T) {
		got := captureEnv(t, []string{"eas", "build"}, []string{"FOO=bar"})
		_, ok := findEnv(got, EASWorkingDirEnv)
		assert.False(t, ok, "no injection without HOME — we have no safe path to pin")
	})
}

func Test_parseCommand(t *testing.T) {
	cases := []struct {
		args            []string
		expectedCommand string
	}{
		{[]string{"yarn", "build:ios", "-v"}, "yarn build:ios"},
		{[]string{"npm", "run", "start"}, "npm run start"},
		{[]string{"npm", "run", "build:ios", "--", "--verbose"}, "npm run build:ios"},
		{[]string{"npx", "react-native", "run-ios"}, "npx react-native run-ios"},
		{[]string{"npx", "create-expo-app", "my-app"}, "npx create-expo-app"},
		{[]string{"expo", "build:ios"}, "expo build:ios"},
		{[]string{"pnpm", "install"}, "pnpm install"},
		{[]string{"fastlane", "beta"}, "fastlane beta"},
		{[]string{}, ""},
		{[]string{"unknown-binary"}, "unknown-binary"},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.expectedCommand, parseCommand(tc.args), "args: %v", tc.args)
	}
}
