package proxy

import (
	"context"
	"time"
)

// SessionMeta is the identity of the session being emitted.
type SessionMeta struct {
	InvocationID string
	AppSlug      string
	BuildSlug    string
	StepSlug     string
	StartTime    time.Time
}

// SessionStats is the counters snapshot at emit time.
type SessionStats struct {
	Hits          int64
	Misses        int64
	KVHits        int64
	KVMisses      int64
	Uploads       int64
	UploadBytes   int64
	DownloadBytes int64
	KVUploadBytes int64
}

// InvocationEmitter emits a slim analytics invocation for a closed proxy session.
// F2 (enrichment from xcodebuild) PUTs to the same InvocationID and overwrites this row.
type InvocationEmitter interface {
	EmitSlim(ctx context.Context, meta SessionMeta, stats SessionStats)
}

// HitRate mirrors cmd/xcode/xcodebuild.go — KV counters take priority over blob when both exist.
func (s SessionStats) HitRate() float32 {
	if s.KVHits+s.KVMisses > 0 {
		return float32(s.KVHits) / float32(s.KVHits+s.KVMisses)
	}

	if s.Hits+s.Misses > 0 {
		return float32(s.Hits) / float32(s.Hits+s.Misses)
	}

	return 0
}

func (s stats) toPublic() SessionStats {
	return SessionStats{
		Hits:          s.hits,
		Misses:        s.misses,
		KVHits:        s.kvHits,
		KVMisses:      s.kvMisses,
		Uploads:       s.uploads,
		UploadBytes:   s.uploadBytes,
		DownloadBytes: s.downloadBytes,
		KVUploadBytes: s.kvUploadBytes,
	}
}
