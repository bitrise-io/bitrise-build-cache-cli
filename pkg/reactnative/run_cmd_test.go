//go:build unit

package reactnative

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

func newTestRunner(params RunnerParams) *Runner {
	r := NewRunner(params)
	r.socket = nil
	r.postRun = nil

	return r
}

func TestRunner_Run(t *testing.T) {
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
		err := r.Run([]string{"bash", "-c", "echo hello"}, "", []string{})

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
		err := r.Run([]string{"true"}, "", []string{"EXISTING=value"})

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
			_ = r.Run([]string{"true"}, "", []string{})

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

		err := r.Run([]string{"true"}, "", []string{})
		assert.ErrorIs(t, err, execErr)
	})

	t.Run("missing args returns error", func(t *testing.T) {
		r := newTestRunner(RunnerParams{
			ExecFn: func(_ []string, _ string, _ ...string) error { return nil },
		})

		err := r.Run([]string{}, "", []string{})
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
		err := r.Run([]string{"true"}, fixedID, []string{})

		require.NoError(t, err)
		assert.Equal(t, fixedID, capturedEnvID)
	})
}

func Test_runPostHook(t *testing.T) {
	t.Run("sends run invocation with metadata", func(t *testing.T) {
		var sentInvocation multiplatform.Invocation

		hook := &postRunHookMock{
			getMetadataFunc:   func() common.CacheConfigMetadata { return common.CacheConfigMetadata{BitriseAppID: "app-1"} },
			getAuthConfigFunc: func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{WorkspaceID: "ws-1"}, nil },
			sendInvocationFunc: func(inv multiplatform.Invocation) error {
				sentInvocation = inv

				return nil
			},
		}

		runPostHook(hook, "inv-1", []string{"myapp", "--flag"}, time.Second, nil, "")

		assert.Equal(t, "inv-1", sentInvocation.InvocationID)
		assert.Equal(t, "ws-1", sentInvocation.BitriseWorkspaceSlug)
		assert.Equal(t, "app-1", sentInvocation.BitriseAppSlug)
		assert.Equal(t, "myapp", sentInvocation.Command)
		assert.Equal(t, "myapp --flag", sentInvocation.FullCommand)
		assert.True(t, sentInvocation.Success)
		assert.Equal(t, "react-native", sentInvocation.BuildTool)
	})

	t.Run("reports success=false when exec fails", func(t *testing.T) {
		var sentInvocation multiplatform.Invocation
		execErr := errors.New("build failed")

		hook := &postRunHookMock{
			getAuthConfigFunc:  func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
			sendInvocationFunc: func(inv multiplatform.Invocation) error { sentInvocation = inv; return nil },
		}

		runPostHook(hook, "inv-2", []string{"true"}, time.Second, execErr, "")

		assert.False(t, sentInvocation.Success)
		assert.Contains(t, sentInvocation.Error, "build failed")
	})

	t.Run("command is runner+subcommand for known package managers", func(t *testing.T) {
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
		}

		for _, tc := range cases {
			var sentInvocation multiplatform.Invocation

			hook := &postRunHookMock{
				getAuthConfigFunc:  func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
				sendInvocationFunc: func(inv multiplatform.Invocation) error { sentInvocation = inv; return nil },
			}

			runPostHook(hook, "inv", tc.args, 0, nil, "")
			assert.Equal(t, tc.expectedCommand, sentInvocation.Command, "args: %v", tc.args)
		}
	})

	t.Run("getAuthConfig error aborts the hook", func(t *testing.T) {
		sendCalled := false

		hook := &postRunHookMock{
			getAuthConfigFunc:  func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, errors.New("no auth") },
			sendInvocationFunc: func(_ multiplatform.Invocation) error { sendCalled = true; return nil },
		}

		runPostHook(hook, "inv", []string{"true"}, 0, nil, "")
		assert.False(t, sendCalled)
	})

	t.Run("sendRelation is not called when sendInvocation fails", func(t *testing.T) {
		relationCalled := false

		hook := &postRunHookMock{
			getAuthConfigFunc:  func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
			sendInvocationFunc: func(_ multiplatform.Invocation) error { return errors.New("analytics down") },
			sendRelationFunc:   func(_ context.Context, _, _ string) { relationCalled = true },
		}

		runPostHook(hook, "inv", []string{"true"}, 0, nil, "ccache-id")
		assert.False(t, relationCalled)
	})
}
