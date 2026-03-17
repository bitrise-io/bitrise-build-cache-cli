//go:build unit

package reactnative_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/reactnative"
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
			}, noNotify)
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
		)
		require.NoError(t, err)
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
		)

		assert.Error(t, err)
	})
}
