package ccache

import (
	"fmt"
	"sync/atomic"
	"time"
)

type callMethod string

const (
	CALL_METHOD_GET               callMethod = "Get"
	CALL_METHOD_PUT               callMethod = "Set"
	CALL_METHOD_REMOVE            callMethod = "Remove"
	CALL_METHOD_STOP              callMethod = "Stop"
	CALL_METHOD_SET_INVOCATION_ID callMethod = "SetInvocationID"
)

type callStats struct {
	start         time.Time
	method        callMethod
	key           string
	uploadBytes   int64
	downloadBytes int64
}

type statBuilder struct {
	stats callStats
}

func newStatBuilder(method callMethod) *statBuilder {
	return &statBuilder{
		stats: callStats{
			method: method,
			start:  time.Now(),
		},
	}
}

func (b *statBuilder) withKey(key string) {
	b.stats.key = key
}

func (b *statBuilder) withUploadBytes(bytes int64) *statBuilder {
	b.stats.uploadBytes = bytes

	return b
}

func (b *statBuilder) withDownloadBytes(bytes int64) *statBuilder {
	b.stats.downloadBytes = bytes

	return b
}

func (b *statBuilder) build() callStats {
	return b.stats
}

func (b *statBuilder) Prefix() string {
	if b.stats.key == "" {
		return fmt.Sprintf("[%s]", b.stats.method)
	}

	return fmt.Sprintf("[%s - %s]", b.stats.method, b.stats.key)
}

// sessionState aggregates byte-transfer counters across all requests handled by a server instance.
type sessionState struct {
	downloadBytes atomic.Int64
	uploadBytes   atomic.Int64
}

func newSessionState() *sessionState {
	return &sessionState{}
}

func (s *sessionState) resetAndGet() (int64, int64) {
	return s.downloadBytes.Swap(0), s.uploadBytes.Swap(0)
}

func (s *sessionState) updateWithResult(result processResult) {
	if result.Outcome != PROCESS_REQUEST_OK {
		return
	}

	switch result.CallStats.method {
	case CALL_METHOD_GET:
		s.downloadBytes.Add(result.CallStats.downloadBytes)

	case CALL_METHOD_PUT:
		s.uploadBytes.Add(result.CallStats.uploadBytes)

	case CALL_METHOD_REMOVE, CALL_METHOD_STOP, CALL_METHOD_SET_INVOCATION_ID:
		// no byte tracking for these methods
	}
}
