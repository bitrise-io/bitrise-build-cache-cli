package enrichment

import "time"

// xcactivitylog lands after the proxy socket has gone idle.
const correlationSlack = 30 * time.Second

// Max-time-overlap heuristic: two concurrent builds with overlapping windows can
// attach the manifest to the wrong InvocationID. Fine for the serial case; revisit
// if we see mis-attribution in production.
func Correlate(entry ManifestEntry, pending []PendingRecord) (string, bool) {
	if entry.Start.IsZero() || entry.Stop.IsZero() {
		return "", false
	}

	var (
		bestID      string
		bestOverlap time.Duration
	)

	for _, p := range pending {
		pStart := p.StartTime
		pStop := p.StartTime.Add(time.Duration(p.Duration)*time.Millisecond + correlationSlack)

		overlap := intervalOverlap(entry.Start, entry.Stop, pStart, pStop)
		if overlap <= 0 {
			continue
		}

		if overlap > bestOverlap {
			bestOverlap = overlap
			bestID = p.InvocationID
		}
	}

	return bestID, bestID != ""
}

func intervalOverlap(aStart, aStop, bStart, bStop time.Time) time.Duration {
	start := aStart
	if bStart.After(start) {
		start = bStart
	}

	stop := aStop
	if bStop.Before(stop) {
		stop = bStop
	}

	return stop.Sub(start)
}
