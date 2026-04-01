//go:build unit

package reactnative_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/reactnative"
)

func TestBuildEnsureCcacheHelperFn(t *testing.T) {
	const socketPath = "/tmp/test.sock"

	t.Run("silently skips when ccache is not configured", func(t *testing.T) {
		startCalled := false

		fn := reactnative.BuildEnsureCcacheHelperFn(
			func() (string, error) { return "", errors.New("not configured") },
			func(string) bool { return false },
			func() error { startCalled = true; return nil },
			func(string) bool { return true },
		)

		fn()

		assert.False(t, startCalled, "start should not be called when ccache is not configured")
	})

	t.Run("does not start helper when socket is already listening", func(t *testing.T) {
		startCalled := false

		fn := reactnative.BuildEnsureCcacheHelperFn(
			func() (string, error) { return socketPath, nil },
			func(string) bool { return true }, // already listening
			func() error { startCalled = true; return nil },
			func(string) bool { return true },
		)

		fn()

		assert.False(t, startCalled, "start should not be called when socket is already listening")
	})

	t.Run("starts helper and waits when socket is not listening", func(t *testing.T) {
		startCalled := false
		awaitCalled := false

		fn := reactnative.BuildEnsureCcacheHelperFn(
			func() (string, error) { return socketPath, nil },
			func(string) bool { return false }, // not listening
			func() error { startCalled = true; return nil },
			func(string) bool { awaitCalled = true; return true },
		)

		fn()

		assert.True(t, startCalled)
		assert.True(t, awaitCalled)
	})

	t.Run("does not await when start helper fails", func(t *testing.T) {
		awaitCalled := false

		fn := reactnative.BuildEnsureCcacheHelperFn(
			func() (string, error) { return socketPath, nil },
			func(string) bool { return false },
			func() error { return errors.New("start failed") },
			func(string) bool { awaitCalled = true; return true },
		)

		fn()

		assert.False(t, awaitCalled)
	})

	t.Run("passes correct socket path to awaitReadyFn", func(t *testing.T) {
		var awaitedPath string

		fn := reactnative.BuildEnsureCcacheHelperFn(
			func() (string, error) { return socketPath, nil },
			func(string) bool { return false },
			func() error { return nil },
			func(p string) bool { awaitedPath = p; return true },
		)

		fn()

		assert.Equal(t, socketPath, awaitedPath)
	})
}
