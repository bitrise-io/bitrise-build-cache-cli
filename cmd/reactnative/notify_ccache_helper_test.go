//go:build unit

package reactnative_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/reactnative"
)

func TestEnsureCcacheHelperDeps(t *testing.T) {
	const socketPath = "/tmp/test.sock"

	t.Run("silently skips when ccache is not configured", func(t *testing.T) {
		startCalled := false

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return "", errors.New("not configured") },
			IsListening: func(string) bool { return false },
			StartHelper: func() error { startCalled = true; return nil },
			AwaitReady:  func(string) bool { return true },
		}.Build()

		fn()

		assert.False(t, startCalled, "start should not be called when ccache is not configured")
	})

	t.Run("does not start helper when socket is already listening", func(t *testing.T) {
		startCalled := false

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return socketPath, nil },
			IsListening: func(string) bool { return true }, // already listening
			StartHelper: func() error { startCalled = true; return nil },
			AwaitReady:  func(string) bool { return true },
		}.Build()

		fn()

		assert.False(t, startCalled, "start should not be called when socket is already listening")
	})

	t.Run("starts helper and waits when socket is not listening", func(t *testing.T) {
		startCalled := false
		awaitCalled := false

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return socketPath, nil },
			IsListening: func(string) bool { return false }, // not listening
			StartHelper: func() error { startCalled = true; return nil },
			AwaitReady:  func(string) bool { awaitCalled = true; return true },
		}.Build()

		fn()

		assert.True(t, startCalled)
		assert.True(t, awaitCalled)
	})

	t.Run("does not await when start helper fails", func(t *testing.T) {
		awaitCalled := false

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return socketPath, nil },
			IsListening: func(string) bool { return false },
			StartHelper: func() error { return errors.New("start failed") },
			AwaitReady:  func(string) bool { awaitCalled = true; return true },
		}.Build()

		fn()

		assert.False(t, awaitCalled)
	})

	t.Run("passes correct socket path to AwaitReady", func(t *testing.T) {
		var awaitedPath string

		fn := reactnative.EnsureCcacheHelperDeps{
			SocketPath:  func() (string, error) { return socketPath, nil },
			IsListening: func(string) bool { return false },
			StartHelper: func() error { return nil },
			AwaitReady:  func(p string) bool { awaitedPath = p; return true },
		}.Build()

		fn()

		assert.Equal(t, socketPath, awaitedPath)
	})
}
