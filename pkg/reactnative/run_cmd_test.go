//go:build unit

package reactnative

import (
	"context"
	"errors"
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

func TestRunner_Run(t *testing.T) {
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
