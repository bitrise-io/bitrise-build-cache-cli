package enrichment

import (
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

// TickAttempt records that a PUT attempt is about to fire. Safe to call with
// a nil HealthWriter (no-op). Errors are logged via logger and swallowed —
// enrichment health is diagnostic, never a hard requirement.
func TickAttempt(h *HealthWriter, logger log.Logger, now time.Time) {
	if h == nil {
		return
	}
	if err := h.Update(func(s *HealthSnapshot) {
		s.LastAttempt = now
	}); err != nil {
		logOr(logger).Warnf("Failed to record enrichment attempt health: %s", err)
	}
}

// TickSuccess records a successful PUT. matched=true additionally bumps
// LastMatched — the watcher's correlated path is the one that proves the
// enrichment pipeline still lines up manifests to pending records. The wrapper
// self-enrich path ticks matched=false.
func TickSuccess(h *HealthWriter, logger log.Logger, now time.Time, matched bool) {
	if h == nil {
		return
	}
	if err := h.Update(func(s *HealthSnapshot) {
		s.LastSuccess = now
		if matched {
			s.LastMatched = now
		}
		s.ConsecutiveErrors = 0
		s.LastError = ""
		s.LastErrorAt = time.Time{}
	}); err != nil {
		logOr(logger).Warnf("Failed to record enrichment success health: %s", err)
	}
}

// TickFailure records a PUT failure. Increments ConsecutiveErrors so the
// doctor's warning threshold can trip.
func TickFailure(h *HealthWriter, logger log.Logger, now time.Time, putErr error) {
	if h == nil {
		return
	}
	if err := h.Update(func(s *HealthSnapshot) {
		s.LastError = putErr.Error()
		s.LastErrorAt = now
		s.ConsecutiveErrors++
	}); err != nil {
		logOr(logger).Warnf("Failed to record enrichment failure health: %s", err)
	}
}
