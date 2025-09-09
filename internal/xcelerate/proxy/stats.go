package proxy

import (
	"sync"
	"sync/atomic"
)

type sessionState struct {
	downloadBytes atomic.Int64
	uploadBytes   atomic.Int64
	hits          atomic.Int64
	misses        atomic.Int64
	savedKeys     sync.Map
}

type stats struct {
	downloadBytes int64
	uploadBytes   int64
	misses        int64
	hits          int64
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

func (s *sessionState) getStats() stats {
	return stats{
		downloadBytes: s.downloadBytes.Load(),
		uploadBytes:   s.uploadBytes.Load(),
		hits:          s.hits.Load(),
		misses:        s.misses.Load(),
	}
}

func (s *sessionState) incrementMisses() {
	s.misses.Add(1)
}

func (s *sessionState) incrementHits() {
	s.hits.Add(1)
}

func (s *sessionState) isKeyAlreadySaved(key string) bool {
	_, loaded := s.savedKeys.LoadOrStore(key, struct{}{})

	return loaded
}

func (s *sessionState) markKeyUnsaved(key string) {
	s.savedKeys.Delete(key)
}
