//go:build unit

package ccache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_IpcServer_SessionBytes(t *testing.T) {
	t.Run("returns accumulated download and upload bytes from session state", func(t *testing.T) {
		s := &IpcServer{sessionState: newSessionState()}
		s.sessionState.downloadBytes.Store(1024)
		s.sessionState.uploadBytes.Store(4096)

		dl, ul := s.SessionBytes()

		assert.Equal(t, int64(1024), dl)
		assert.Equal(t, int64(4096), ul)
	})

	t.Run("returns zero when no transfers have occurred", func(t *testing.T) {
		s := &IpcServer{sessionState: newSessionState()}

		dl, ul := s.SessionBytes()

		assert.Equal(t, int64(0), dl)
		assert.Equal(t, int64(0), ul)
	})

	t.Run("reflects reset after SetInvocationID", func(t *testing.T) {
		s := &IpcServer{sessionState: newSessionState()}
		s.sessionState.downloadBytes.Store(512)
		s.sessionState.uploadBytes.Store(1024)

		// This is what handleConnection does when SetInvocationID succeeds
		s.sessionState.reset()

		dl, ul := s.SessionBytes()
		assert.Equal(t, int64(0), dl)
		assert.Equal(t, int64(0), ul)
	})
}
