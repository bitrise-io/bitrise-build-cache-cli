package enrichment

import (
	"encoding/json"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

//go:generate moq -stub -out invocation_putter_mock_test.go -pkg enrichment_test . InvocationPutter

type InvocationPutter interface {
	PutInvocation(inv analytics.Invocation) error
}

// Enricher re-PUTs enriched analytics.Invocation for a manifest entry,
// correlating to a pending record when available (matched path) or minting a
// fresh UUID (orphan path).
type Enricher struct {
	Store            *Store
	Client           InvocationPutter
	Auth             auth.Credential
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

func (e *Enricher) Enrich(group ManifestEntryGroup) {
	logger := logOr(e.Logger)

	if len(group.Entries) == 0 {
		return
	}

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

	command := group.Command()
	if command == "" {
		logger.Debugf("Enrichment PUT skipped for group scheme=%q: side-effect only (uuids=%v)", group.SchemeName(), group.UUIDs())

		return
	}

	invocationID, matched := Correlate(GroupCorrelationSpan(group), pending)
	if !matched {
		invocationID = uuid.NewString()
	}

	if matched && MarkerExists(invocationID) {
		logger.Debugf("Enrichment PUT skipped for %s: wrapper already handled", invocationID)
		// Marker stays — PruneAll reclaims it after HandledMarkerMaxAge. Consume-on-read here broke the wrapper path (slim emit removed the marker before the watcher could see it).
		if e.Store != nil {
			if err := e.Store.Remove(invocationID); err != nil {
				logger.Warnf("Failed to remove pending after skipping enrichment for %s: %s", invocationID, err)
			}
		}

		return
	}

	inv := analytics.NewInvocation(analytics.InvocationRunStats{
		InvocationDate:   group.Start(),
		InvocationID:     invocationID,
		Duration:         group.Duration().Milliseconds(),
		Command:          command,
		FullCommand:      group.FullCommand(),
		Success:          group.Success(),
		XcodeVersion:     e.XcodeVersion,
		XcodeBuildNumber: e.XcodeBuildNumber,
	}, e.Auth, e.Metadata)

	TickAttempt(e.Health, e.Logger, e.now())

	if err := e.Client.PutInvocation(*inv); err != nil {
		logger.Warnf("Failed to PUT enriched invocation %s: %s", invocationID, err)
		TickFailure(e.Health, e.Logger, e.now(), err)
		e.recordFailure(invocationID, matched, inv, err)

		return
	}

	TickSuccess(e.Health, e.Logger, e.now(), matched)

	logger.Infof("Enriched invocation PUT %s (matched=%t scheme=%s cmd=%s entries=%d)", invocationID, matched, group.SchemeName(), command, len(group.Entries))

	if matched && e.Store != nil {
		if err := e.Store.Remove(invocationID); err != nil {
			logger.Warnf("Failed to remove pending invocation %s after enrichment: %s", invocationID, err)
		}
	}
}

// GroupCorrelationSpan collapses a group into a ManifestEntry (aggregate
// Start/Stop over primary metadata) so Correlate can overlap-match against
// pending records. See ManifestEntryGroup for the wide-span trade-off.
func GroupCorrelationSpan(g ManifestEntryGroup) ManifestEntry {
	p := g.Primary()
	p.Start = g.Start()
	p.Stop = g.Stop()

	return p
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

// updatePendingRetry returns true when a record with invocationID existed on
// disk. false means the caller should fall through to appending a fresh
// record. On persist failure we return whatever found was in the callback:
// if the record existed, the untouched on-disk copy is still there for the
// next Retrier sweep; if it didn't, the caller must Append so no failure is
// silently dropped.
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
	}

	return found
}
