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
	savedKeys     sync.Map
}

type stats struct {
	downloadBytes int64
	uploadBytes   int64
	uploads       int64
	misses        int64
	hits          int64
	kvHits        int64
	kvMisses      int64
	kvUploadBytes int64
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
	}
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
