//go:build unit

package reactnative_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/reactnative"
)

func TestBuildNotifyCcacheHelperFn(t *testing.T) {
	const socketPath = "/tmp/test.sock"
	const invocationID = "test-invocation-id"

	t.Run("silently skips when ccache is not configured", func(t *testing.T) {
		startCalled := false
		sendCalled := false

		fn := reactnative.BuildNotifyCcacheHelperFn(
			func() (string, error) { return "", errors.New("not configured") },
			func(string) bool { return false },
			func() error { startCalled = true; return nil },
			func(string) bool { return true },
			func(string, string, string) error { sendCalled = true; return nil },
		)

		fn(invocationID)

		assert.False(t, startCalled, "start should not be called when ccache is not configured")
		assert.False(t, sendCalled, "send should not be called when ccache is not configured")
	})

	t.Run("sends invocation ID when socket is already listening", func(t *testing.T) {
		startCalled := false
		var sentSocket, sentParentID string

		fn := reactnative.BuildNotifyCcacheHelperFn(
			func() (string, error) { return socketPath, nil },
			func(string) bool { return true }, // already listening
			func() error { startCalled = true; return nil },
			func(string) bool { return true },
			func(sock, parentID, _ string) error { sentSocket = sock; sentParentID = parentID; return nil },
		)

		fn(invocationID)

		assert.False(t, startCalled, "start should not be called when socket is already listening")
		assert.Equal(t, socketPath, sentSocket)
		assert.Equal(t, invocationID, sentParentID)
	})

	t.Run("starts helper and waits before sending when socket is not listening", func(t *testing.T) {
		startCalled := false
		awaitCalled := false
		var sentParentID string

		fn := reactnative.BuildNotifyCcacheHelperFn(
			func() (string, error) { return socketPath, nil },
			func(string) bool { return false }, // not listening
			func() error { startCalled = true; return nil },
			func(string) bool { awaitCalled = true; return true },
			func(_, parentID, _ string) error { sentParentID = parentID; return nil },
		)

		fn(invocationID)

		assert.True(t, startCalled)
		assert.True(t, awaitCalled)
		assert.Equal(t, invocationID, sentParentID)
	})

	t.Run("does not send when start helper fails", func(t *testing.T) {
		sendCalled := false

		fn := reactnative.BuildNotifyCcacheHelperFn(
			func() (string, error) { return socketPath, nil },
			func(string) bool { return false },
			func() error { return errors.New("start failed") },
			func(string) bool { return true },
			func(string, string, string) error { sendCalled = true; return nil },
		)

		fn(invocationID)

		assert.False(t, sendCalled)
	})

	t.Run("does not send when helper does not become ready", func(t *testing.T) {
		sendCalled := false

		fn := reactnative.BuildNotifyCcacheHelperFn(
			func() (string, error) { return socketPath, nil },
			func(string) bool { return false },
			func() error { return nil },
			func(string) bool { return false }, // never becomes ready
			func(string, string, string) error { sendCalled = true; return nil },
		)

		fn(invocationID)

		assert.False(t, sendCalled)
	})

	t.Run("passes correct socket path to awaitReadyFn and sendInvocationIDFn", func(t *testing.T) {
		var awaitedPath, sentPath string

		fn := reactnative.BuildNotifyCcacheHelperFn(
			func() (string, error) { return socketPath, nil },
			func(string) bool { return false },
			func() error { return nil },
			func(p string) bool { awaitedPath = p; return true },
			func(p, _, _ string) error { sentPath = p; return nil },
		)

		fn(invocationID)

		assert.Equal(t, socketPath, awaitedPath)
		assert.Equal(t, socketPath, sentPath)
	})
}
