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
	// EndTime is the wall-clock of the last RPC on this session (or zero if
	// none were observed). Emit callers use it to compute Duration accurately —
	// otherwise inactivity-timer flushes overreport by the idle window.
	EndTime time.Time
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
// The enrichment watcher may re-PUT to the same InvocationID for wrapper-less builds; wrapper builds skip via the handled-invocation marker.
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
