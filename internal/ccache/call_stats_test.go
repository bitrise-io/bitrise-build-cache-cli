//go:build unit

package ccache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_sessionState_resetAndGet(t *testing.T) {
	t.Run("returns accumulated bytes and zeroes counters", func(t *testing.T) {
		s := newSessionState()
		s.downloadBytes.Store(1024)
		s.uploadBytes.Store(2048)

		dl, ul := s.resetAndGet()

		assert.Equal(t, int64(1024), dl)
		assert.Equal(t, int64(2048), ul)
		assert.Equal(t, int64(0), s.downloadBytes.Load())
		assert.Equal(t, int64(0), s.uploadBytes.Load())
	})

	t.Run("resetAndGet on already-zero state is safe", func(t *testing.T) {
		s := newSessionState()

		dl, ul := s.resetAndGet()

		assert.Equal(t, int64(0), dl)
		assert.Equal(t, int64(0), ul)
	})
}

func Test_sessionState_updateWithResult(t *testing.T) {
	t.Run("GET OK adds download bytes", func(t *testing.T) {
		s := newSessionState()
		result := processResult{
			Outcome: PROCESS_REQUEST_OK,
			CallStats: callStats{
				method:        CALL_METHOD_GET,
				downloadBytes: 1024,
			},
		}

		s.updateWithResult(result)

		assert.Equal(t, int64(1024), s.downloadBytes.Load())
		assert.Equal(t, int64(0), s.uploadBytes.Load())
	})

	t.Run("GET MISS does not change any counters", func(t *testing.T) {
		s := newSessionState()
		result := processResult{
			Outcome: PROCESS_REQUEST_MISS,
			CallStats: callStats{
				method: CALL_METHOD_GET,
			},
		}

		s.updateWithResult(result)

		assert.Equal(t, int64(0), s.downloadBytes.Load())
		assert.Equal(t, int64(0), s.uploadBytes.Load())
	})

	t.Run("PUT OK adds upload bytes", func(t *testing.T) {
		s := newSessionState()
		result := processResult{
			Outcome: PROCESS_REQUEST_OK,
			CallStats: callStats{
				method:      CALL_METHOD_PUT,
				uploadBytes: 2048,
			},
		}

		s.updateWithResult(result)

		assert.Equal(t, int64(0), s.downloadBytes.Load())
		assert.Equal(t, int64(2048), s.uploadBytes.Load())
	})

	t.Run("ERROR on GET does not change any counters", func(t *testing.T) {
		s := newSessionState()
		result := processResult{
			Outcome: PROCESS_REQUEST_ERROR,
			CallStats: callStats{
				method: CALL_METHOD_GET,
			},
		}

		s.updateWithResult(result)

		assert.Equal(t, int64(0), s.downloadBytes.Load())
		assert.Equal(t, int64(0), s.uploadBytes.Load())
	})
}
