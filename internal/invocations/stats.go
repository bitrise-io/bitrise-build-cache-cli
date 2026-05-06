package invocations

import (
	"encoding/json"
	"fmt"
	"sort"
)

// AggregateFromList computes count + hit-rate P50 + estimated time-saved by
// paging through `Client.List` and aggregating the presenter-shape items
// client-side.
//
// Used as the productisation fallback while the bitrise-website middle layer
// doesn't proxy the new `/internal/invocations/stats` endpoint (ACI-4911):
// callers get the same shape as `DirectClient.Stats`, just from data that's
// reachable via the public website API.
//
// Behaviour notes:
//   - Caps at `maxItems` (default 1000 when ≤0). Big workspaces / long
//     windows can have orders of magnitude more invocations — this is
//     a hackathon-shaped approximation, not a billing-grade aggregate.
//   - `cacheHitRate` may be null per ACI-4914; null rows are excluded from
//     the median sample and contribute 0 to time-saved.
//   - `duration` unit is opaque per ACI-4914. xcode rows in prod are small
//     floats (e.g. 2.254) — we treat as seconds and multiply by 1000 to
//     report milliseconds. Same heuristic the seed-from-prod replayer uses.
//
// `filter.Page` and `filter.ItemsPerPage` are ignored — paging is driven
// internally.
func AggregateFromList(client *Client, filter ListFilter, maxItems int) (*InvocationStats, error) {
	if maxItems <= 0 {
		maxItems = 1000
	}

	const pageSize = 100

	stats := &InvocationStats{}

	hitRates := make([]float64, 0, maxItems)
	timeSavedMs := int64(0)
	totalCount := uint64(0)

	page := 1

	for len(hitRates) < maxItems {
		queryFilter := filter
		queryFilter.Page = page
		queryFilter.ItemsPerPage = pageSize

		resp, err := client.List(queryFilter)
		if err != nil {
			return nil, fmt.Errorf("aggregate page %d: %w", page, err)
		}

		// Authoritative count comes from the first page's paging.totalCount —
		// no point re-asking.
		if page == 1 {
			totalCount = uint64(resp.Paging.TotalCount) //nolint:gosec
		}

		if len(resp.Items) == 0 {
			break
		}

		for _, raw := range resp.Items {
			var item InvocationSummary
			if err := json.Unmarshal(raw, &item); err != nil {
				// Skip malformed items rather than failing the whole aggregate;
				// validator (cmd/xcelr8validate) is the right place to surface
				// drift.
				continue
			}

			if item.CacheHitRate != nil {
				hr := *item.CacheHitRate
				hitRates = append(hitRates, hr)

				if item.Duration != nil {
					// Treat presenter `duration` as seconds (see types.go
					// note); convert to ms for the time-saved estimate.
					timeSavedMs += int64(*item.Duration * hr * 1000.0)
				}
			}
		}

		// Last page reached.
		if len(resp.Items) < pageSize {
			break
		}

		page++
	}

	stats.Count = totalCount
	stats.TimeSavedMs = timeSavedMs
	stats.HitRateP50 = medianFloat64(hitRates)

	return stats, nil
}

func medianFloat64(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}

	sorted := make([]float64, len(xs))
	copy(sorted, xs)
	sort.Float64s(sorted)

	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}

	return (sorted[mid-1] + sorted[mid]) / 2.0
}
