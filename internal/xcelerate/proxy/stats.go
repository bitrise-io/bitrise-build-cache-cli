package proxy

import (
	"sync"
	"sync/atomic"
)

type statsCollector struct {
	downloadBytes atomic.Int64
	uploadBytes   atomic.Int64
	misses        sync.Map
	hits          sync.Map
}

type stats struct {
	downloadBytes int64
	uploadBytes   int64
	misses        int64
	hits          int64
}

func newStatsCollector() *statsCollector {
	return &statsCollector{}
}

func (s *statsCollector) addDownloadBytes(n int64) {
	s.downloadBytes.Add(n)
}

func (s *statsCollector) addUploadBytes(n int64) {
	s.uploadBytes.Add(n)
}

func (s *statsCollector) getStats() stats {
	return stats{
		downloadBytes: s.downloadBytes.Load(),
		uploadBytes:   s.uploadBytes.Load(),
		misses:        s.countMap(&s.misses),
		hits:          s.countMap(&s.hits),
	}
}

func (s *statsCollector) addMiss(key string) {
	s.misses.Store(key, struct{}{})
}

func (s *statsCollector) addHit(key string) {
	s.hits.Store(key, struct{}{})
}

func (s *statsCollector) countMap(m *sync.Map) int64 {
	var count int64

	m.Range(func(_, _ any) bool {
		count++

		return true
	})

	return count
}
