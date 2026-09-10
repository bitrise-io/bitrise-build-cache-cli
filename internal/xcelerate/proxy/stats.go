package proxy

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

type sessionState struct {
	// blobStats is the single source of truth for every counter getStats reports: each hit,
	// miss, upload, byte and error is one of its records, recorded against the protocol it
	// travelled on, so the totals and the KV subset cannot drift from each other.
	blobStats *blobstats.ProtocolCollector

	firstError atomic.Pointer[string]
	savedKeys  sync.Map
}

const errorMessageMax = 300

// cacheOp names one proxy RPC. Typed because it selects which protocol a record is filed under,
// so a mistyped name has to fail the build rather than skew the KV subset.
type cacheOp string

const (
	opGet      cacheOp = "Get"
	opPut      cacheOp = "Put"
	opLoad     cacheOp = "Load"
	opSave     cacheOp = "Save"
	opGetValue cacheOp = "GetValue"
	opPutValue cacheOp = "PutValue"
)

type stats struct {
	downloadBytes int64
	uploadBytes   int64
	uploads       int64
	misses        int64
	hits          int64
	kvHits        int64
	kvMisses      int64
	kvUploadBytes int64
	errors        int64
	firstError    string
	blobStats     blobstats.Snapshot
}

func newSessionState() *sessionState {
	//nolint:exhaustruct // the atomic and the key map start zeroed
	return &sessionState{blobStats: blobstats.NewProtocolCollector()}
}

func (s *sessionState) getStats() stats {
	blobStats := s.blobStats.Snapshot()

	// hits/misses fold CAS and KV together; the kv* fields are the KV subset, which is what
	// defines the session hit rate — a KV lookup is one compilation-cache-key decision, while
	// a CAS get fetches a blob that decision already pointed at, so counting blobs overstates
	// the rate. kvUploadBytes only feeds the wrapper's summary log line.
	return stats{
		downloadBytes: blobStats.Download.BytesTotal,
		uploadBytes:   blobStats.Upload.BytesTotal,
		uploads:       blobStats.Upload.OpCount,
		hits:          blobStats.Download.OpCount,
		misses:        blobStats.Download.MissCount,
		kvHits:        blobStats.KV.Download.OpCount,
		kvMisses:      blobStats.KV.Download.MissCount,
		kvUploadBytes: blobStats.KV.Upload.BytesTotal,
		errors:        blobStats.Download.ErrorCount + blobStats.Upload.ErrorCount,
		firstError:    s.loadFirstError(),
		blobStats:     blobStats,
	}
}

// Timed around the cache client call alone: the hashing and gob coding around it is local work.
func (s *sessionState) recordDownload(op cacheOp, bytes int64, duration time.Duration) {
	s.protocolFor(op).Download.RecordTransfer(bytes, duration)
}

func (s *sessionState) recordUpload(op cacheOp, bytes int64, duration time.Duration) {
	s.protocolFor(op).Upload.RecordTransfer(bytes, duration)
}

func (s *sessionState) recordMiss(op cacheOp) {
	s.protocolFor(op).Download.RecordMiss()
}

func (s *sessionState) recordError(op cacheOp, err error) {
	if isDownloadOp(op) {
		s.protocolFor(op).Download.RecordError()
	} else {
		s.protocolFor(op).Upload.RecordError()
	}

	msg := string(op) + ": " + err.Error()
	if len(msg) > errorMessageMax {
		msg = msg[:errorMessageMax] + "…"
	}
	// First writer wins; later errors only add to the count.
	s.firstError.CompareAndSwap(nil, &msg)
}

func (s *sessionState) loadFirstError() string {
	if p := s.firstError.Load(); p != nil {
		return *p
	}

	return ""
}

func (s *sessionState) saveKeyOnce(key string) bool {
	_, loaded := s.savedKeys.LoadOrStore(key, struct{}{})

	return loaded
}

func (s *sessionState) markKeyUnsaved(key string) {
	s.savedKeys.Delete(key)
}

// recordSkippedAlreadySaved keeps local dedup apart from the transfers: timing it would make
// the latency distribution incomparable with Gradle's.
func (s *sessionState) recordSkippedAlreadySaved(op cacheOp) {
	s.protocolFor(op).Upload.RecordSkippedAlreadySaved()
}

func (s *sessionState) protocolFor(op cacheOp) *blobstats.Collector {
	if isKVOp(op) {
		return s.blobStats.KV
	}

	return s.blobStats.CAS
}

func isDownloadOp(op cacheOp) bool {
	switch op {
	case opGet, opLoad, opGetValue:
		return true
	case opPut, opSave, opPutValue:
		return false
	default:
		return false
	}
}

func isKVOp(op cacheOp) bool {
	return op == opGetValue || op == opPutValue
}
