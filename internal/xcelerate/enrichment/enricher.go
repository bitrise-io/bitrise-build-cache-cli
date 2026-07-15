package enrichment

import (
	"encoding/json"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

//go:generate moq -stub -out invocation_putter_mock_test.go -pkg enrichment_test . InvocationPutter

type InvocationPutter interface {
	PutInvocation(inv analytics.Invocation) error
}

// Xcode.app never calls SetSession so no pending match; mint a fresh id.
type Enricher struct {
	Store            *Store
	Client           InvocationPutter
	Auth             configcommon.CacheAuthConfig
	Metadata         configcommon.CacheConfigMetadata
	XcodeVersion     string
	XcodeBuildNumber string
	Logger           log.Logger
	Health           *HealthWriter
	Now              func() time.Time
}

func (e *Enricher) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}

	return time.Now()
}

func (e *Enricher) Enrich(entry ManifestEntry) {
	logger := logOr(e.Logger)

	var (
		pending []PendingRecord
		err     error
	)

	if e.Store != nil {
		pending, err = e.Store.Load()
		if err != nil {
			logger.Warnf("Failed to load pending invocations for enrichment: %s", err)
		}
	}

	invocationID, matched := Correlate(entry, pending)
	if !matched {
		invocationID = uuid.NewString()
	}

	if matched && MarkerExists(invocationID) {
		logger.Debugf("Enrichment PUT skipped for %s: wrapper already handled", invocationID)
		RemoveMarker(invocationID)
		if e.Store != nil {
			if err := e.Store.Remove(invocationID); err != nil {
				logger.Warnf("Failed to remove pending after skipping enrichment for %s: %s", invocationID, err)
			}
		}

		return
	}

	inv := analytics.NewInvocation(analytics.InvocationRunStats{
		InvocationDate:   entry.Start,
		InvocationID:     invocationID,
		Duration:         entry.Stop.Sub(entry.Start).Milliseconds(),
		Command:          string(entry.Command()),
		FullCommand:      entry.Signature,
		Success:          entry.Success(),
		XcodeVersion:     e.XcodeVersion,
		XcodeBuildNumber: e.XcodeBuildNumber,
	}, e.Auth, e.Metadata)

	if scheme := entry.SchemeName; scheme != "" {
		inv.Command = string(entry.Command()) + " " + scheme
	}

	e.markAttempt()

	if err := e.Client.PutInvocation(*inv); err != nil {
		logger.Warnf("Failed to PUT enriched invocation %s: %s", invocationID, err)
		e.markFailure(err)
		e.recordFailure(invocationID, matched, inv, err)

		return
	}

	e.markSuccess()

	logger.Debugf("Enriched invocation PUT %s (matched=%t scheme=%s cmd=%s)", invocationID, matched, entry.SchemeName, entry.Command())

	if matched && e.Store != nil {
		if err := e.Store.Remove(invocationID); err != nil {
			logger.Warnf("Failed to remove pending invocation %s after enrichment: %s", invocationID, err)
		}
	}
}

func (e *Enricher) markAttempt() {
	if e.Health == nil {
		return
	}

	now := e.now()
	if err := e.Health.Update(func(s *HealthSnapshot) {
		s.LastAttempt = now
	}); err != nil {
		logOr(e.Logger).Warnf("Failed to record enrichment attempt health: %s", err)
	}
}

func (e *Enricher) markSuccess() {
	if e.Health == nil {
		return
	}

	now := e.now()
	if err := e.Health.Update(func(s *HealthSnapshot) {
		s.LastSuccess = now
		s.ConsecutiveErrors = 0
		s.LastError = ""
		s.LastErrorAt = time.Time{}
	}); err != nil {
		logOr(e.Logger).Warnf("Failed to record enrichment success health: %s", err)
	}
}

func (e *Enricher) markFailure(putErr error) {
	if e.Health == nil {
		return
	}

	now := e.now()
	if err := e.Health.Update(func(s *HealthSnapshot) {
		s.LastError = putErr.Error()
		s.LastErrorAt = now
		s.ConsecutiveErrors++
	}); err != nil {
		logOr(e.Logger).Warnf("Failed to record enrichment failure health: %s", err)
	}
}

func (e *Enricher) recordFailure(invocationID string, matched bool, inv *analytics.Invocation, putErr error) {
	if e.Store == nil {
		return
	}

	logger := logOr(e.Logger)

	payload, err := json.Marshal(inv)
	if err != nil {
		logger.Warnf("Failed to marshal enriched invocation %s for retry: %s", invocationID, err)

		return
	}

	if matched && e.updatePendingRetry(invocationID, payload, putErr) {
		return
	}

	now := e.now()
	rec := PendingRecord{
		InvocationID:    invocationID,
		FirstAttempt:    now,
		LastAttempt:     now,
		Attempts:        1,
		LastError:       putErr.Error(),
		EnrichedPayload: payload,
	}
	if err := e.Store.Append(rec); err != nil {
		logger.Warnf("Failed to append orphan retry record %s: %s", invocationID, err)
	}
}

// updatePendingRetry returns true when a record with invocationID existed and
// its retry state was updated (or persisting failed and we should stop). false
// means the caller should fall through to appending a fresh record.
func (e *Enricher) updatePendingRetry(invocationID string, payload []byte, putErr error) bool {
	logger := logOr(e.Logger)
	now := e.now()
	found := false

	if err := e.Store.Mutate(func(existing []PendingRecord) []PendingRecord {
		for i := range existing {
			if existing[i].InvocationID != invocationID {
				continue
			}
			if existing[i].FirstAttempt.IsZero() {
				existing[i].FirstAttempt = now
			}
			existing[i].LastAttempt = now
			existing[i].Attempts++
			existing[i].LastError = putErr.Error()
			existing[i].EnrichedPayload = payload
			found = true

			break
		}

		return existing
	}); err != nil {
		logger.Warnf("Failed to persist retry state for %s: %s", invocationID, err)

		return true
	}

	return found
}
