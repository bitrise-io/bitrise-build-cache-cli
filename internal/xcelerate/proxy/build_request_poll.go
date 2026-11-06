package proxy

import "time"

// buildRequestPollInterval is the cadence at which the proxy scans for new
// <hex>.xcbuilddata entries under each IDE peer's DerivedData. 1s matches
// Xcode's own build-start latency closely enough that an initial RPC on a
// fresh ⌘B lands on the reseated InvocationID in all but pathological races;
// a slower cadence would group two neighbouring ⌘B's onto one BE row.
const buildRequestPollInterval = 1 * time.Second

// pollAllForReseat iterates every outstanding session and asks the handler to
// reseat the InvocationID when the build-request hex changed. Returns the
// count of sessions whose ID was reseated, for test observability.
func (h *sidecarStatsHandler) pollAllForReseat() int {
	if h == nil || h.registry == nil {
		return 0
	}

	h.registry.mu.Lock()
	sessions := make([]*connectionSession, 0, len(h.registry.byKey))
	for _, cs := range h.registry.byKey {
		sessions = append(sessions, cs)
	}
	h.registry.mu.Unlock()

	reseated := 0
	for _, cs := range sessions {
		if h.ResolveAndMaybeReseat(cs) {
			reseated++
		}
	}

	return reseated
}
