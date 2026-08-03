package proxy

import (
	"sync"
	"sync/atomic"
)

type sessionState struct {
	downloadBytes atomic.Int64
	uploadBytes   atomic.Int64
	uploads       atomic.Int64
	hits          atomic.Int64
	misses        atomic.Int64
	kvHits        atomic.Int64
	kvMisses      atomic.Int64
	kvUploadBytes atomic.Int64
	errors        atomic.Int64
	firstError    atomic.Pointer[string]
	savedKeys     sync.Map
}

const errorMessageMax = 300

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
}

func newSessionState() *sessionState {
	return &sessionState{}
}

func (s *sessionState) addDownloadBytes(n int64) {
	s.downloadBytes.Add(n)
}

func (s *sessionState) addUploadBytes(n int64) {
	s.uploadBytes.Add(n)
}

func (s *sessionState) addKVUploadBytes(n int64) {
	s.kvUploadBytes.Add(n)
}

func (s *sessionState) getStats() stats {
	return stats{
		downloadBytes: s.downloadBytes.Load(),
		uploadBytes:   s.uploadBytes.Load(),
		uploads:       s.uploads.Load(),
		hits:          s.hits.Load(),
		misses:        s.misses.Load(),
		kvHits:        s.kvHits.Load(),
		kvMisses:      s.kvMisses.Load(),
		kvUploadBytes: s.kvUploadBytes.Load(),
		errors:        s.errors.Load(),
		firstError:    s.loadFirstError(),
	}
}

func (s *sessionState) recordError(op string, err error) {
	s.errors.Add(1)

	msg := op + ": " + err.Error()
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

func (s *sessionState) incrementMisses() {
	s.misses.Add(1)
}

func (s *sessionState) incrementHits() {
	s.hits.Add(1)
}

func (s *sessionState) incrementKVMisses() {
	s.kvMisses.Add(1)
}

func (s *sessionState) incrementKVHits() {
	s.kvHits.Add(1)
}

func (s *sessionState) incrementUploads() {
	s.uploads.Add(1)
}

func (s *sessionState) saveKeyOnce(key string) bool {
	_, loaded := s.savedKeys.LoadOrStore(key, struct{}{})

	return loaded
}

func (s *sessionState) markKeyUnsaved(key string) {
	s.savedKeys.Delete(key)
}
