package proxy

import (
	"sync/atomic"
)

type stats struct {
	downloadBytes atomic.Int64
	uploadBytes   atomic.Int64
}

func newStats() *stats {
	return &stats{}
}

func (s *stats) addDownloadBytes(n int64) {
	s.downloadBytes.Add(n)
}

func (s *stats) addUploadBytes(n int64) {
	s.uploadBytes.Add(n)
}

func (s *stats) getBytes() (int64, int64) {
	return s.downloadBytes.Load(), s.uploadBytes.Load()
}
