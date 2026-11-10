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
// Mirrors the wrapper's precedence; see cmd/xcode/xcodebuild.go:getHitRateFromSessionAndRunStats.
// Keep in lockstep.
//
// DirectionSnapshot.OpCount counts successful transfers (hits), not total
// attempts — RecordMiss() bumps MissCount only, leaving OpCount at zero. So
// the denominator is OpCount + MissCount, matching the wrapper's
// hits / (hits + misses) formula.
func computeHitRate(bucket Bucket) float32 {
	var hr float32

	if bs := bucket.BlobStats; bs != nil {
		if total := bs.Download.OpCount + bs.Download.MissCount; total > 0 {
			hr = float32(bs.Download.OpCount) / float32(total)
		}
		if bs.KV != nil {
			if total := bs.KV.Download.OpCount + bs.KV.Download.MissCount; total > 0 {
				hr = float32(bs.KV.Download.OpCount) / float32(total)
			}
		}
	}

	if bucket.XCActivityLogPath != "" {
		if m, err := xcactivitylog.ReadCompilationCacheMetrics(bucket.XCActivityLogPath); err == nil && m.Total > 0 {
			hr = m.HitRate
		}
	}

	return hr
}
