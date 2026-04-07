//go:build unit

package reactnative_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/reactnative"
)

const (
	testSocketPath        = "/tmp/test.sock"
	testRNInvocationID    = "rn-invocation-id"
	testCcacheInvocationID = "ccache-invocation-id"
)

func TestEnsureCcacheHelperDeps(t *testing.T) {
	t.Run("silently skips when ccache is not configured", func(t *testing.T) {
		startCalled := false

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return "", errors.New("not configured") },
			IsListening: func(string) bool { return false },
			StartHelper: func() error { startCalled = true; return nil },
			AwaitReady:  func(string) bool { return true },
		}.Build()

		fn(testRNInvocationID, testCcacheInvocationID)

		assert.False(t, startCalled, "start should not be called when ccache is not configured")
	})

	t.Run("does not start helper when socket is already listening", func(t *testing.T) {
		startCalled := false

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return testSocketPath, nil },
			IsListening: func(string) bool { return true }, // already listening
			StartHelper: func() error { startCalled = true; return nil },
			AwaitReady:  func(string) bool { return true },
		}.Build()

		fn(testRNInvocationID, testCcacheInvocationID)

		assert.False(t, startCalled, "start should not be called when socket is already listening")
	})

	t.Run("starts helper and waits when socket is not listening", func(t *testing.T) {
		startCalled := false
		awaitCalled := false

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return testSocketPath, nil },
			IsListening: func(string) bool { return false }, // not listening
			StartHelper: func() error { startCalled = true; return nil },
			AwaitReady:  func(string) bool { awaitCalled = true; return true },
		}.Build()

		fn(testRNInvocationID, testCcacheInvocationID)

		assert.True(t, startCalled)
		assert.True(t, awaitCalled)
	})

	t.Run("does not await when start helper fails", func(t *testing.T) {
		awaitCalled := false

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return testSocketPath, nil },
			IsListening: func(string) bool { return false },
			StartHelper: func() error { return errors.New("start failed") },
			AwaitReady:  func(string) bool { awaitCalled = true; return true },
		}.Build()

		fn(testRNInvocationID, testCcacheInvocationID)

		assert.False(t, awaitCalled)
	})

	t.Run("continues without error when AwaitReady returns false", func(t *testing.T) {
		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return testSocketPath, nil },
			IsListening: func(string) bool { return false },
			StartHelper: func() error { return nil },
			AwaitReady:  func(string) bool { return false },
		}.Build()

		// should not panic or return error — just logs a warning and continues
		fn(testRNInvocationID, testCcacheInvocationID)
	})

	t.Run("passes correct socket path to AwaitReady", func(t *testing.T) {
		var awaitedPath string

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return testSocketPath, nil },
			IsListening: func(string) bool { return false },
			StartHelper: func() error { return nil },
			AwaitReady:  func(p string) bool { awaitedPath = p; return true },
		}.Build()

		fn(testRNInvocationID, testCcacheInvocationID)

		assert.Equal(t, testSocketPath, awaitedPath)
	})

	t.Run("calls HealthCheck with the socket path when helper is ready", func(t *testing.T) {
		var healthCheckedPath string

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:   func() (string, error) { return testSocketPath, nil },
			IsListening:  func(string) bool { return true },
			StartHelper:  func() error { return nil },
			AwaitReady:   func(string) bool { return true },
			HealthCheck:  func(_ context.Context, p string) error { healthCheckedPath = p; return nil },
		}.Build()

		fn(testRNInvocationID, testCcacheInvocationID)

		assert.Equal(t, testSocketPath, healthCheckedPath)
	})

	t.Run("continues when HealthCheck fails", func(t *testing.T) {
		sendInvocationCalled := false

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:       func() (string, error) { return testSocketPath, nil },
			IsListening:      func(string) bool { return true },
			StartHelper:      func() error { return nil },
			AwaitReady:       func(string) bool { return true },
			HealthCheck:      func(_ context.Context, _ string) error { return errors.New("unhealthy") },
			SendInvocationID: func(_ context.Context, _, _, _ string) error { sendInvocationCalled = true; return nil },
		}.Build()

		fn(testRNInvocationID, testCcacheInvocationID)

		assert.True(t, sendInvocationCalled, "SendInvocationID should still be called after a failed health check")
	})

	t.Run("calls SendInvocationID with correct IDs", func(t *testing.T) {
		var gotSocketPath, gotParentID, gotChildID string

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return testSocketPath, nil },
			IsListening: func(string) bool { return true },
			StartHelper: func() error { return nil },
			AwaitReady:  func(string) bool { return true },
			SendInvocationID: func(_ context.Context, socketPath, parentID, childID string) error {
				gotSocketPath = socketPath
				gotParentID = parentID
				gotChildID = childID
				return nil
			},
		}.Build()

		fn(testRNInvocationID, testCcacheInvocationID)

		assert.Equal(t, testSocketPath, gotSocketPath)
		assert.Equal(t, testRNInvocationID, gotParentID)
		assert.Equal(t, testCcacheInvocationID, gotChildID)
	})

	t.Run("does not call HealthCheck or SendInvocationID when start helper fails", func(t *testing.T) {
		healthCheckCalled := false
		sendInvocationCalled := false

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:       func() (string, error) { return testSocketPath, nil },
			IsListening:      func(string) bool { return false },
			StartHelper:      func() error { return errors.New("start failed") },
			AwaitReady:       func(string) bool { return true },
			HealthCheck:      func(_ context.Context, _ string) error { healthCheckCalled = true; return nil },
			SendInvocationID: func(_ context.Context, _, _, _ string) error { sendInvocationCalled = true; return nil },
		}.Build()

		fn(testRNInvocationID, testCcacheInvocationID)

		assert.False(t, healthCheckCalled)
		assert.False(t, sendInvocationCalled)
	})
}
