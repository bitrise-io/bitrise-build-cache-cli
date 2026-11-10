package wrapperless

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
)

// computeHitRate picks the enriched row's hit rate across three tiers,
// highest priority wins:
//
//  1. blob — proxy CAS GETs
//  2. KV   — proxy KV GETs (overlays blob; KV is cache-key decisions, not blob
//     fetches those decisions cause, so it reflects the user signal more
//     faithfully)
//  3. xcactivitylog's CompilationCacheMetrics — Xcode's own log-parsed number,
//     the only signal that survives when the proxy observes zero CAS traffic
//     (warm local cache). Overlays KV because Xcode counts cacheable tasks,
//     not cache requests.
//
// Matches getHitRateFromSessionAndRunStats in cmd/xcode/xcodebuild.go — the
// wrapper flow's precedence. Keep the two in lockstep.
func computeHitRate(bucket Bucket) float32 {
	var hr float32

	if bs := bucket.BlobStats; bs != nil {
		if bs.Download.OpCount > 0 {
			hits := bs.Download.OpCount - bs.Download.MissCount
			hr = float32(hits) / float32(bs.Download.OpCount)
		}
		if bs.KV != nil && bs.KV.Download.OpCount > 0 {
			kvHits := bs.KV.Download.OpCount - bs.KV.Download.MissCount
			hr = float32(kvHits) / float32(bs.KV.Download.OpCount)
		}
	}

	if bucket.XCActivityLogPath != "" {
		if m, err := xcactivitylog.ReadCompilationCacheMetrics(bucket.XCActivityLogPath); err == nil && m.Total > 0 {
			hr = m.HitRate
		}
	}

	return hr
}
