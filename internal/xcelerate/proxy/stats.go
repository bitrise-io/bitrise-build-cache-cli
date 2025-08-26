package proxy

import (
	"sync/atomic"
)

type statsCollector struct {
	downloadBytes atomic.Int64
	uploadBytes   atomic.Int64
	hits          atomic.Int64
	misses        atomic.Int64
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
		hits:          s.hits.Load(),
		misses:        s.misses.Load(),
	}
}

func (s *statsCollector) incrementMisses() {
	s.misses.Add(1)
}

func (s *statsCollector) incrementHits() {
	s.hits.Add(1)
}
