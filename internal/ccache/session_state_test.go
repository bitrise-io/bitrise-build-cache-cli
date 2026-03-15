//go:build unit

package ccache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_sessionState_updateWithResult(t *testing.T) {
	t.Run("GET OK increments hits and adds download bytes", func(t *testing.T) {
		s := newSessionState()
		result := processResult{
			Outcome: PROCESS_REQUEST_OK,
			CallStats: callStats{
				method:        CALL_METHOD_GET,
				downloadBytes: 1024,
			},
		}

		s.updateWithResult(result)

		assert.Equal(t, int64(1), s.hits.Load())
		assert.Equal(t, int64(0), s.misses.Load())
		assert.Equal(t, int64(1024), s.downloadBytes.Load())
		assert.Equal(t, int64(0), s.uploadBytes.Load())
	})

	t.Run("GET MISS increments misses only", func(t *testing.T) {
		s := newSessionState()
		result := processResult{
			Outcome: PROCESS_REQUEST_MISS,
			CallStats: callStats{
				method: CALL_METHOD_GET,
			},
		}

		s.updateWithResult(result)

		assert.Equal(t, int64(0), s.hits.Load())
		assert.Equal(t, int64(1), s.misses.Load())
		assert.Equal(t, int64(0), s.downloadBytes.Load())
		assert.Equal(t, int64(0), s.uploadBytes.Load())
	})

	t.Run("PUT OK adds upload bytes only", func(t *testing.T) {
		s := newSessionState()
		result := processResult{
			Outcome: PROCESS_REQUEST_OK,
			CallStats: callStats{
				method:      CALL_METHOD_PUT,
				uploadBytes: 2048,
			},
		}

		s.updateWithResult(result)

		assert.Equal(t, int64(0), s.hits.Load())
		assert.Equal(t, int64(0), s.misses.Load())
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

		assert.Equal(t, int64(0), s.hits.Load())
		assert.Equal(t, int64(0), s.misses.Load())
		assert.Equal(t, int64(0), s.downloadBytes.Load())
		assert.Equal(t, int64(0), s.uploadBytes.Load())
	})
}
