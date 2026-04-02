//go:build unit

package ccache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

func Test_NewServer_initializes_activeInvocationID(t *testing.T) {
	server, err := NewServer(
		ccacheconfig.Config{},
		configcommon.CacheConfigMetadata{},
		&ClientMock{},
		mockLogger,
		nil,
		"my-initial-id",
		nil,
	)
	require.NoError(t, err)

	server.activeInvocationMu.Lock()
	got := server.activeInvocationID
	server.activeInvocationMu.Unlock()

	assert.Equal(t, "my-initial-id", got)
}

func Test_handleSetInvocationIDResult(t *testing.T) {
	t.Run("updates active ID and resets stats on new invocation", func(t *testing.T) {
		s := &IpcServer{sessionState: newSessionState()}
		s.sessionState.downloadBytes.Store(100)
		s.activeInvocationID = "old-id"

		s.handleSetInvocationIDResult(processResult{InvocationChildID: "new-id"})

		s.activeInvocationMu.Lock()
		gotID := s.activeInvocationID
		s.activeInvocationMu.Unlock()

		assert.Equal(t, "new-id", gotID)
		dl, _ := s.SessionBytes()
		assert.Equal(t, int64(0), dl, "stats should be reset on new invocation")
	})

	t.Run("duplicate invocation ID does not reset stats or change active ID", func(t *testing.T) {
		s := &IpcServer{sessionState: newSessionState()}
		s.sessionState.downloadBytes.Store(200)
		s.activeInvocationID = "same-id"

		s.handleSetInvocationIDResult(processResult{InvocationChildID: "same-id"})

		s.activeInvocationMu.Lock()
		gotID := s.activeInvocationID
		s.activeInvocationMu.Unlock()

		assert.Equal(t, "same-id", gotID)
		dl, _ := s.SessionBytes()
		assert.Equal(t, int64(200), dl, "stats should not be reset for duplicate invocation ID")
	})
}

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
		s.sessionState.resetAndGet()

		dl, ul := s.SessionBytes()
		assert.Equal(t, int64(0), dl)
		assert.Equal(t, int64(0), ul)
	})
}
