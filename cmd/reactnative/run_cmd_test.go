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
	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/analytics"
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
			}, noNotify, nil)
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
		)

		assert.Error(t, err)
	})
}

func Test_BuildCcacheAnalyticsHooksFn(t *testing.T) {
	noopExecFn := func(_ []string, _ string, _ ...string) error { return nil }

	t.Run("skips both hooks when ccache is not found", func(t *testing.T) {
		resetCalled := false
		collectCalled := false

		hooks := reactnative.BuildCcacheAnalyticsHooksFn(
			func() (string, bool) { return "", false },
			func(_ string) error { resetCalled = true; return nil },
			func(_ string) ([]byte, error) { collectCalled = true; return nil, nil },
			func() common.CacheConfigMetadata { return common.CacheConfigMetadata{} },
			func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
			func(_ ccacheanalytics.Invocation) error { return nil },
		)

		_ = reactnative.RunCmdFn([]string{"true"}, []string{}, noopExecFn, nil, hooks)

		assert.False(t, resetCalled)
		assert.False(t, collectCalled)
	})

	t.Run("resets stats before exec and collects+sends after", func(t *testing.T) {
		resetCalled := false
		collectCalled := false
		var sentInvocation ccacheanalytics.Invocation

		statsJSON := []byte(`{"stats":{"cache_hit_direct":3,"cache_hit_preprocessed":1,"cache_miss":6,"cache_hit_rate":0.4,"errors_compiling":0,"files_in_cache":10,"cache_size_kibibyte":512}}`)

		hooks := reactnative.BuildCcacheAnalyticsHooksFn(
			func() (string, bool) { return "/usr/bin/ccache", true },
			func(_ string) error { resetCalled = true; return nil },
			func(_ string) ([]byte, error) { collectCalled = true; return statsJSON, nil },
			func() common.CacheConfigMetadata {
				return common.CacheConfigMetadata{BitriseAppID: "app-1"}
			},
			func() (common.CacheAuthConfig, error) {
				return common.CacheAuthConfig{WorkspaceID: "ws-1"}, nil
			},
			func(inv ccacheanalytics.Invocation) error {
				sentInvocation = inv
				return nil
			},
		)

		_ = reactnative.RunCmdFn([]string{"myapp", "--flag"}, []string{}, noopExecFn, nil, hooks)

		assert.True(t, resetCalled)
		assert.True(t, collectCalled)
		assert.NotEmpty(t, sentInvocation.InvocationID)
		assert.Equal(t, "ws-1", sentInvocation.BitriseOrgSlug)
		assert.Equal(t, "app-1", sentInvocation.BitriseAppSlug)
		assert.Equal(t, "myapp", sentInvocation.Command)
		assert.Equal(t, "myapp --flag", sentInvocation.FullCommand)
		assert.True(t, sentInvocation.Success)
		assert.Equal(t, 3, sentInvocation.CcacheStats.CacheHitDirect)
		assert.Equal(t, 1, sentInvocation.CcacheStats.CacheHitPreprocessed)
		assert.Equal(t, 6, sentInvocation.CcacheStats.CacheMiss)
		assert.InDelta(t, 0.4, sentInvocation.CcacheStats.CacheHitRate, 0.001)
	})

	t.Run("reports success=false when exec fails", func(t *testing.T) {
		statsJSON := []byte(`{"stats":{}}`)
		var sentInvocation ccacheanalytics.Invocation
		execErr := errors.New("build failed")

		hooks := reactnative.BuildCcacheAnalyticsHooksFn(
			func() (string, bool) { return "/usr/bin/ccache", true },
			func(_ string) error { return nil },
			func(_ string) ([]byte, error) { return statsJSON, nil },
			func() common.CacheConfigMetadata { return common.CacheConfigMetadata{} },
			func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
			func(inv ccacheanalytics.Invocation) error { sentInvocation = inv; return nil },
		)

		_ = reactnative.RunCmdFn(
			[]string{"true"},
			[]string{},
			func(_ []string, _ string, _ ...string) error { return execErr },
			nil,
			hooks,
		)

		assert.False(t, sentInvocation.Success)
		assert.Contains(t, sentInvocation.Error, "build failed")
	})

	t.Run("skips send when collect stats fails", func(t *testing.T) {
		sendCalled := false

		hooks := reactnative.BuildCcacheAnalyticsHooksFn(
			func() (string, bool) { return "/usr/bin/ccache", true },
			func(_ string) error { return nil },
			func(_ string) ([]byte, error) { return nil, errors.New("ccache unavailable") },
			func() common.CacheConfigMetadata { return common.CacheConfigMetadata{} },
			func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
			func(_ ccacheanalytics.Invocation) error { sendCalled = true; return nil },
		)

		_ = reactnative.RunCmdFn([]string{"true"}, []string{}, noopExecFn, nil, hooks)

		assert.False(t, sendCalled)
	})

	t.Run("duration is positive and invocation date precedes now", func(t *testing.T) {
		statsJSON := []byte(`{"stats":{}}`)
		var sentInvocation ccacheanalytics.Invocation
		before := time.Now()

		hooks := reactnative.BuildCcacheAnalyticsHooksFn(
			func() (string, bool) { return "/usr/bin/ccache", true },
			func(_ string) error { return nil },
			func(_ string) ([]byte, error) { return statsJSON, nil },
			func() common.CacheConfigMetadata { return common.CacheConfigMetadata{} },
			func() (common.CacheAuthConfig, error) { return common.CacheAuthConfig{}, nil },
			func(inv ccacheanalytics.Invocation) error { sentInvocation = inv; return nil },
		)

		_ = reactnative.RunCmdFn([]string{"true"}, []string{}, noopExecFn, nil, hooks)

		assert.True(t, sentInvocation.DurationMs >= 0)
		assert.True(t, sentInvocation.InvocationDate.Before(time.Now()))
		assert.True(t, !sentInvocation.InvocationDate.Before(before))
	})
}
