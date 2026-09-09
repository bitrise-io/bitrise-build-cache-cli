//go:build unit

package ccache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func getResult(outcome processResultOutcome, downloadBytes int64) processResult {
	return processResult{
		Outcome: outcome,
		CallStats: callStats{
			method:        CALL_METHOD_GET,
			downloadBytes: downloadBytes,
			transfer:      time.Millisecond,
		},
	}
}

func putResult(outcome processResultOutcome, uploadBytes int64) processResult {
	return processResult{
		Outcome: outcome,
		CallStats: callStats{
			method:      CALL_METHOD_PUT,
			uploadBytes: uploadBytes,
			transfer:    time.Millisecond,
		},
	}
}

func Test_sessionState_takeEffectiveness(t *testing.T) {
	t.Run("returns the invocation's summary and zeroes it", func(t *testing.T) {
		s := newSessionState()
		s.updateWithResult(getResult(PROCESS_REQUEST_OK, 1024))
		s.updateWithResult(putResult(PROCESS_REQUEST_OK, 2048))

		outgoing := s.takeEffectiveness()

		assert.Equal(t, int64(1024), outgoing.DownloadBytes)
		assert.Equal(t, int64(2048), outgoing.UploadBytes)
		assert.Equal(t, CacheEffectiveness{}, s.effectiveness())

		dl, ul := s.sessionBytes()
		assert.Zero(t, dl)
		assert.Zero(t, ul)
	})

	t.Run("on already-zero state is safe", func(t *testing.T) {
		assert.Equal(t, CacheEffectiveness{}, newSessionState().takeEffectiveness())
	})
}

func Test_sessionState_updateWithResult(t *testing.T) {
	t.Run("GET OK adds download bytes", func(t *testing.T) {
		s := newSessionState()
		s.updateWithResult(getResult(PROCESS_REQUEST_OK, 1024))

		dl, ul := s.sessionBytes()
		assert.Equal(t, int64(1024), dl)
		assert.Zero(t, ul)
	})

	t.Run("GET MISS counts a miss without moving bytes", func(t *testing.T) {
		s := newSessionState()
		s.updateWithResult(getResult(PROCESS_REQUEST_MISS, 0))

		dl, ul := s.sessionBytes()
		assert.Zero(t, dl)
		assert.Zero(t, ul)
		assert.Equal(t, int64(1), s.blobStatsSnapshot().Download.MissCount)
	})

	t.Run("PUT OK adds upload bytes", func(t *testing.T) {
		s := newSessionState()
		s.updateWithResult(putResult(PROCESS_REQUEST_OK, 2048))

		dl, ul := s.sessionBytes()
		assert.Zero(t, dl)
		assert.Equal(t, int64(2048), ul)
	})

	t.Run("ERROR on GET moves no bytes but counts against the direction", func(t *testing.T) {
		s := newSessionState()
		s.updateWithResult(getResult(PROCESS_REQUEST_ERROR, 0))

		dl, ul := s.sessionBytes()
		assert.Zero(t, dl)
		assert.Zero(t, ul)
		assert.Equal(t, int64(1), s.blobStatsSnapshot().Download.ErrorCount)
	})

	// The summary line counts these, so they must not be folded into a transfer direction.
	t.Run("a non-transfer error counts in errors but not in either direction", func(t *testing.T) {
		s := newSessionState()
		s.updateWithResult(processResult{
			Outcome:   PROCESS_REQUEST_ERROR,
			CallStats: callStats{method: CALL_METHOD_REMOVE},
		})

		assert.Equal(t, int64(1), s.effectiveness().Errors)
		assert.True(t, s.blobStatsSnapshot().IsEmpty())
	})
}

func Test_sessionState_effectiveness(t *testing.T) {
	t.Run("counts hits, misses and errors of the current invocation", func(t *testing.T) {
		s := newSessionState()
		s.updateWithResult(getResult(PROCESS_REQUEST_OK, 1024))
		s.updateWithResult(getResult(PROCESS_REQUEST_MISS, 0))
		s.updateWithResult(putResult(PROCESS_REQUEST_OK, 2048))
		s.updateWithResult(putResult(PROCESS_REQUEST_ERROR, 0))

		assert.Equal(t, CacheEffectiveness{
			Hits:          1,
			Total:         2,
			Errors:        1,
			DownloadBytes: 1024,
			UploadBytes:   2048,
		}, s.effectiveness())

		s.takeEffectiveness()

		assert.Equal(t, CacheEffectiveness{}, s.effectiveness())
	})

	t.Run("empty session", func(t *testing.T) {
		assert.Equal(t, CacheEffectiveness{}, newSessionState().effectiveness())
	})
}
