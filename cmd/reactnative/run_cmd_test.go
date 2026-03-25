//go:build unit

package reactnative_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/reactnative"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

func Test_RunWithInvocationIDFn(t *testing.T) {
	noNotify := func(string) {}

	t.Run("args are passed through to the command", func(t *testing.T) {
		var capturedName string
		var capturedArgs []string

		err := reactnative.RunWithInvocationIDFn(
			[]string{"bash", "-c", "echo hello"},
			[]string{},
			func(_ []string, name string, args ...string) error {
				capturedName = name
				capturedArgs = args

				return nil
			},
			noNotify,
			nil,
			nil,
		)

		require.NoError(t, err)
		assert.Equal(t, "bash", capturedName)
		assert.Equal(t, []string{"-c", "echo hello"}, capturedArgs)
	})

	t.Run("BITRISE_INVOCATION_ID is injected into the environment", func(t *testing.T) {
		var capturedEnviron []string

		err := reactnative.RunWithInvocationIDFn(
			[]string{"true"},
			[]string{"EXISTING=value"},
			func(environ []string, _ string, _ ...string) error {
				capturedEnviron = environ

				return nil
			},
			noNotify,
			nil,
			nil,
		)

		require.NoError(t, err)
		assert.Contains(t, capturedEnviron, "EXISTING=value")

		var invocationIDEntry string
		for _, e := range capturedEnviron {
			if strings.HasPrefix(e, "BITRISE_INVOCATION_ID=") {
				invocationIDEntry = e

				break
			}
		}
		assert.NotEmpty(t, invocationIDEntry, "BITRISE_INVOCATION_ID should be set in environment")
		assert.NotEqual(t, "BITRISE_INVOCATION_ID=", invocationIDEntry, "invocation ID value should not be empty")
	})

	t.Run("each call generates a distinct invocation ID", func(t *testing.T) {
		extractID := func() string {
			var id string
			_ = reactnative.RunWithInvocationIDFn([]string{"true"}, []string{}, func(environ []string, _ string, _ ...string) error {
				for _, e := range environ {
					if strings.HasPrefix(e, "BITRISE_INVOCATION_ID=") {
						id = strings.TrimPrefix(e, "BITRISE_INVOCATION_ID=")
					}
				}

				return nil
			}, noNotify, nil, nil)

			return id
		}

		id1 := extractID()
		id2 := extractID()
		assert.NotEmpty(t, id1)
		assert.NotEmpty(t, id2)
		assert.NotEqual(t, id1, id2)
	})

	t.Run("notifyFn is called with the invocation ID", func(t *testing.T) {
		var notifiedID string
		var envID string

		err := reactnative.RunWithInvocationIDFn(
			[]string{"true"},
			[]string{},
			func(environ []string, _ string, _ ...string) error {
				for _, e := range environ {
					if strings.HasPrefix(e, "BITRISE_INVOCATION_ID=") {
						envID = strings.TrimPrefix(e, "BITRISE_INVOCATION_ID=")
					}
				}

				return nil
			},
			func(id string) { notifiedID = id },
			nil,
			nil,
		)

		require.NoError(t, err)
		assert.NotEmpty(t, notifiedID)
		assert.Equal(t, envID, notifiedID, "notifyFn should receive the same ID injected into the environment")
	})

	t.Run("nil notifyFn is safe", func(t *testing.T) {
		err := reactnative.RunWithInvocationIDFn(
			[]string{"true"},
			[]string{},
			func(_ []string, _ string, _ ...string) error { return nil },
			nil,
			nil,
			nil,
		)
		require.NoError(t, err)
	})

	t.Run("preRunFn is called before execution", func(t *testing.T) {
		var preRunCalled bool
		var execCalled bool

		err := reactnative.RunWithInvocationIDFn(
			[]string{"true"},
			[]string{},
			func(_ []string, _ string, _ ...string) error {
				execCalled = true

				return nil
			},
			noNotify,
			func() { preRunCalled = true },
			nil,
		)

		require.NoError(t, err)
		assert.True(t, preRunCalled)
		assert.True(t, execCalled)
	})

	t.Run("error from execFn is propagated", func(t *testing.T) {
		execErr := errors.New("exec failed")

		err := reactnative.RunWithInvocationIDFn(
			[]string{"true"},
			[]string{},
			func(_ []string, _ string, _ ...string) error {
				return execErr
			},
			noNotify,
			nil,
			nil,
		)

		assert.ErrorIs(t, err, execErr)
	})

	t.Run("missing args returns error", func(t *testing.T) {
		err := reactnative.RunWithInvocationIDFn(
			[]string{},
			[]string{},
			func(_ []string, _ string, _ ...string) error {
				return nil
			},
			noNotify,
			nil,
			nil,
		)

		assert.Error(t, err)
	})
}

func Test_BuildPostRunFn(t *testing.T) {
	noopExecFn := func(_ []string, _ string, _ ...string) error { return nil }

	t.Run("sends run invocation with metadata", func(t *testing.T) {
		var sentInvocation multiplatform.Invocation

		hooks := reactnative.BuildPostRunFn(
			func() common.CacheConfigMetadata {
				return common.CacheConfigMetadata{BitriseAppID: "app-1"}
			},
			func() (common.CacheAuthConfig, error) {
				return common.CacheAuthConfig{WorkspaceID: "ws-1"}, nil
			},
			func(inv multiplatform.Invocation) error { sentInvocation = inv; return nil },
		)

		_ = reactnative.RunWithInvocationIDFn([]string{"myapp", "--flag"}, []string{}, noopExecFn, nil, nil, hooks)

		assert.NotEmpty(t, sentInvocation.InvocationID)
		assert.Equal(t, "ws-1", sentInvocation.BitriseOrgSlug)
		assert.Equal(t, "app-1", sentInvocation.BitriseAppSlug)
		assert.Equal(t, "myapp", sentInvocation.Command)
		assert.Equal(t, "myapp --flag", sentInvocation.FullCommand)
		assert.True(t, sentInvocation.Success)
		assert.Equal(t, "react-native", sentInvocation.Wrapper)
	})

	t.Run("reports success=false when exec fails", func(t *testing.T) {
		var sentInvocation multiplatform.Invocation
		execErr := errors.New("build failed")

		hooks := reactnative.BuildPostRunFn(
			func() common.CacheConfigMetadata { return common.CacheConfigMetadata{} },
			func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
			func(inv multiplatform.Invocation) error { sentInvocation = inv; return nil },
		)

		_ = reactnative.RunWithInvocationIDFn(
			[]string{"true"},
			[]string{},
			func(_ []string, _ string, _ ...string) error { return execErr },
			nil,
			nil,
			hooks,
		)

		assert.False(t, sentInvocation.Success)
		assert.Contains(t, sentInvocation.Error, "build failed")
	})

	t.Run("command is runner+subcommand for known package managers", func(t *testing.T) {
		cases := []struct {
			args            []string
			expectedCommand string
		}{
			{[]string{"yarn", "build:ios", "-v", "--stuff=true"}, "yarn build:ios"},
			{[]string{"npm", "run", "start"}, "npm run start"},
			{[]string{"npm", "run", "build:ios", "--", "--verbose"}, "npm run build:ios"},
			{[]string{"npm", "install"}, "npm install"},
			{[]string{"npx", "react-native", "run-ios"}, "npx react-native run-ios"},
			{[]string{"npx", "react-native", "run-android", "--variant=release"}, "npx react-native run-android"},
			{[]string{"npx", "create-expo-app", "my-app"}, "npx create-expo-app"},
			{[]string{"expo", "build:ios"}, "expo build:ios"},
			{[]string{"pnpm", "install"}, "pnpm install"},
			{[]string{"fastlane", "beta"}, "fastlane beta"},
		}

		for _, tc := range cases {
			var sentInvocation multiplatform.Invocation

			hooks := reactnative.BuildPostRunFn(
				func() common.CacheConfigMetadata { return common.CacheConfigMetadata{} },
				func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
				func(inv multiplatform.Invocation) error { sentInvocation = inv; return nil },
			)

			_ = reactnative.RunWithInvocationIDFn(tc.args, []string{}, noopExecFn, nil, nil, hooks)

			assert.Equal(t, tc.expectedCommand, sentInvocation.Command, "args: %v", tc.args)
		}
	})

	t.Run("command is first arg for unknown runners", func(t *testing.T) {
		cases := []struct {
			args []string
		}{
			{[]string{"myapp", "--flag"}},
			{[]string{"./gradlew", "assembleRelease"}},
			{[]string{"make", "build"}},
		}

		for _, tc := range cases {
			var sentInvocation multiplatform.Invocation

			hooks := reactnative.BuildPostRunFn(
				func() common.CacheConfigMetadata { return common.CacheConfigMetadata{} },
				func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
				func(inv multiplatform.Invocation) error { sentInvocation = inv; return nil },
			)

			_ = reactnative.RunWithInvocationIDFn(tc.args, []string{}, noopExecFn, nil, nil, hooks)

			assert.Equal(t, tc.args[0], sentInvocation.Command, "args: %v", tc.args)
		}
	})

	t.Run("command is first arg when package manager has no subcommand", func(t *testing.T) {
		var sentInvocation multiplatform.Invocation

		hooks := reactnative.BuildPostRunFn(
			func() common.CacheConfigMetadata { return common.CacheConfigMetadata{} },
			func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
			func(inv multiplatform.Invocation) error { sentInvocation = inv; return nil },
		)

		_ = reactnative.RunWithInvocationIDFn([]string{"yarn"}, []string{}, noopExecFn, nil, nil, hooks)

		assert.Equal(t, "yarn", sentInvocation.Command)
	})

	t.Run("duration is positive and invocation date precedes now", func(t *testing.T) {
		var sentInvocation multiplatform.Invocation
		before := time.Now()

		hooks := reactnative.BuildPostRunFn(
			func() common.CacheConfigMetadata { return common.CacheConfigMetadata{} },
			func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
			func(inv multiplatform.Invocation) error { sentInvocation = inv; return nil },
		)

		_ = reactnative.RunWithInvocationIDFn([]string{"true"}, []string{}, noopExecFn, nil, nil, hooks)

		assert.True(t, sentInvocation.DurationMs >= 0)
		assert.True(t, sentInvocation.InvocationDate.Before(time.Now()))
		assert.True(t, !sentInvocation.InvocationDate.Before(before))
	})
}
