package ccache

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

type callMethod string

const (
	CALL_METHOD_GET               callMethod = "Get"
	CALL_METHOD_PUT               callMethod = "Set"
	CALL_METHOD_REMOVE            callMethod = "Remove"
	CALL_METHOD_STOP              callMethod = "Stop"
	CALL_METHOD_SET_INVOCATION_ID callMethod = "SetInvocationID"
	CALL_METHOD_GET_SESSION_STATS callMethod = "GetSessionStats"
	CALL_METHOD_GET_BLOB_STATS    callMethod = "GetBlobStats"
	CALL_METHOD_HEALTH_CHECK      callMethod = "HealthCheck"
)

type callStats struct {
	start  time.Time
	method callMethod
	key    string
	// transfer times the cache client call; start also covers reading the request off the socket.
	transfer      time.Duration
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

func (b *statBuilder) withTransfer(duration time.Duration) {
	b.stats.transfer = duration
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

// sessionState aggregates the counters of the invocation currently being served.
type sessionState struct {
	// Single source of truth for the transfer counters; effectiveness derives them.
	blobStats *blobstats.Collector

	// Not derivable: also counts Remove/Stop/SetInvocationID failures, which move no blob.
	errors atomic.Int64
}

func newSessionState() *sessionState {
	//nolint:exhaustruct // the atomics start zeroed
	return &sessionState{blobStats: blobstats.NewCollector()}
}

// takeEffectiveness reads the invocation's summary and starts the next one clean.
func (s *sessionState) takeEffectiveness() CacheEffectiveness {
	outgoing := effectivenessOf(s.blobStats.TakeSnapshot(), s.errors.Swap(0))

	return outgoing
}

func (s *sessionState) blobStatsSnapshot() blobstats.Snapshot {
	return s.blobStats.Snapshot()
}

func (s *sessionState) sessionBytes() (int64, int64) {
	snapshot := s.blobStats.Snapshot()

	return snapshot.Download.BytesTotal, snapshot.Upload.BytesTotal
}

func (s *sessionState) effectiveness() CacheEffectiveness {
	return effectivenessOf(s.blobStats.Snapshot(), s.errors.Load())
}

func effectivenessOf(snapshot blobstats.Snapshot, errors int64) CacheEffectiveness {
	return CacheEffectiveness{
		Hits:          snapshot.Download.OpCount,
		Total:         snapshot.Download.OpCount + snapshot.Download.MissCount,
		Errors:        errors,
		DownloadBytes: snapshot.Download.BytesTotal,
		UploadBytes:   snapshot.Upload.BytesTotal,
	}
}

func (s *sessionState) updateWithResult(result processResult) {
	switch result.Outcome {
	case PROCESS_REQUEST_ERROR:
		s.errors.Add(1)
		s.recordBlobError(result.CallStats.method)
	case PROCESS_REQUEST_MISS:
		if result.CallStats.method == CALL_METHOD_GET {
			s.blobStats.Download.RecordMiss()
		}
	case PROCESS_REQUEST_OK, PROCESS_REQUEST_PUSH_DISABLED:
	}

	if result.Outcome != PROCESS_REQUEST_OK {
		return
	}

	switch result.CallStats.method {
	case CALL_METHOD_GET:
		s.blobStats.Download.RecordTransfer(result.CallStats.downloadBytes, result.CallStats.transfer)

	case CALL_METHOD_PUT:
		s.blobStats.Upload.RecordTransfer(result.CallStats.uploadBytes, result.CallStats.transfer)

	case CALL_METHOD_REMOVE, CALL_METHOD_STOP, CALL_METHOD_SET_INVOCATION_ID, CALL_METHOD_GET_SESSION_STATS, CALL_METHOD_GET_BLOB_STATS, CALL_METHOD_HEALTH_CHECK:
		// no byte tracking for these methods
	}
}

func (s *sessionState) recordBlobError(method callMethod) {
	switch method {
	case CALL_METHOD_GET:
		s.blobStats.Download.RecordError()
	case CALL_METHOD_PUT:
		s.blobStats.Upload.RecordError()
	case CALL_METHOD_REMOVE, CALL_METHOD_STOP, CALL_METHOD_SET_INVOCATION_ID, CALL_METHOD_GET_SESSION_STATS, CALL_METHOD_GET_BLOB_STATS, CALL_METHOD_HEALTH_CHECK:
		// not a blob transfer
	}
}
