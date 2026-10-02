package enrichment

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

//go:generate moq -stub -out invocation_putter_mock_test.go -pkg enrichment_test . InvocationPutter

type InvocationPutter interface {
	PutInvocation(inv analytics.Invocation) error
}

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

	// LogPollMaxWait caps the total wait for the sibling .xcactivitylog. Zero
	// uses defaultLogPollMaxWait. Tests override to keep missing-log cases fast.
	LogPollMaxWait time.Duration
}

// defaultLogPollMaxWait is empirical headroom for the manifest→log gap plus a
// safety margin; the watcher scan is single-goroutine, so blocking too long
// here delays every subsequent group.
const defaultLogPollMaxWait = 2 * time.Second

func (e *Enricher) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}

	return time.Now()
}

// Enrich is the Watcher.Handle callback. manifestPath is the LogStoreManifest.plist
// path the group was parsed from; it anchors the sibling xcactivitylog resolve
// (manifestDir/Primary().FileName) used to read compile-cache metrics into the
// orphan PUT.
func (e *Enricher) Enrich(manifestPath string, group ManifestEntryGroup) {
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

	if pendingID, matched := Correlate(GroupCorrelationSpan(group), pending); matched {
		// Re-PUT would clobber the wrapper's rich row under BE last-write-wins.
		logger.Debugf("Enrichment PUT skipped for %s: pending record already claims this invocation", pendingID)
		if e.Store != nil {
			if err := e.Store.Remove(pendingID); err != nil {
				logger.Warnf("Failed to remove pending after skipping enrichment for %s: %s", pendingID, err)
			}
		}

		return
	}

	invocationID := uuid.NewString()

	hitRate, metricsSource := e.readLogMetrics(manifestPath, group)

	inv := analytics.NewInvocation(analytics.InvocationRunStats{
		InvocationDate:   group.Start(),
		InvocationID:     invocationID,
		Duration:         group.Duration().Milliseconds(),
		Command:          command,
		FullCommand:      group.FullCommand(),
		Success:          group.Success(),
		XcodeVersion:     e.XcodeVersion,
		XcodeBuildNumber: e.XcodeBuildNumber,
		HitRate:          hitRate,
		MetricsSource:    metricsSource,
	}, e.Auth, e.Metadata)

	TickAttempt(e.Health, e.Logger, e.now())

	if err := e.Client.PutInvocation(*inv); err != nil {
		logger.Warnf("Failed to PUT enriched invocation %s: %s", invocationID, err)
		TickFailure(e.Health, e.Logger, e.now(), err)
		e.recordOrphanFailure(invocationID, inv, err)

		return
	}

	// matched=false always: matched groups short-circuit above and LastMatched
	// is reserved for correlated re-PUTs, which no longer happen.
	TickSuccess(e.Health, e.Logger, e.now(), false)

	logger.Infof("Enriched invocation PUT %s (orphan scheme=%s cmd=%s entries=%d)", invocationID, group.SchemeName(), command, len(group.Entries))
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

func (e *Enricher) recordOrphanFailure(invocationID string, inv *analytics.Invocation, putErr error) {
	if e.Store == nil {
		return
	}

	logger := logOr(e.Logger)

	payload, err := json.Marshal(inv)
	if err != nil {
		logger.Warnf("Failed to marshal enriched invocation %s for retry: %s", invocationID, err)

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

// readLogMetrics resolves the sibling xcactivitylog, waits briefly for it to
// land, parses the compile-cache hit rate, and maps the reader Outcome onto a
// MetricsSource tag. An empty manifestPath (seen in tests that construct
// Enricher directly) short-circuits to log_missing without touching disk.
func (e *Enricher) readLogMetrics(manifestPath string, group ManifestEntryGroup) (float32, string) {
	logger := logOr(e.Logger)

	if manifestPath == "" {
		return 0, analytics.MetricsSourceLogMissing
	}

	primary := group.Primary()
	if primary.FileName == "" {
		return 0, analytics.MetricsSourceLogMissing
	}

	logPath := filepath.Join(filepath.Dir(manifestPath), primary.FileName)

	maxWait := e.LogPollMaxWait
	if maxWait == 0 {
		maxWait = defaultLogPollMaxWait
	}
	deadline := e.now().Add(maxWait)

	//exhaustive:ignore // pollLogFile only ever returns OK / FileMissing / ReadError.
	switch e.pollLogFile(logPath, deadline) {
	case xcactivitylog.OutcomeFileMissing:
		return 0, analytics.MetricsSourceLogMissing
	case xcactivitylog.OutcomeReadError:
		return 0, analytics.MetricsSourceLogReadError
	}

	metrics, err := xcactivitylog.ReadCompilationCacheMetricsWithLogger(logPath, logger)
	if err != nil {
		logger.Warnf("xcactivitylog read failed for %s: %s", logPath, err)
	}

	//exhaustive:ignore // FileMissing is handled above by pollLogFile.
	switch metrics.Outcome {
	case xcactivitylog.OutcomeOK:
		return metrics.HitRate, analytics.MetricsSourceActivityLog
	case xcactivitylog.OutcomeEmpty:
		logger.Debugf("xcactivitylog empty at %s", logPath)

		return 0, analytics.MetricsSourceLogEmpty
	case xcactivitylog.OutcomeReadError:
		return 0, analytics.MetricsSourceLogReadError
	case xcactivitylog.OutcomeUnparsed:
		size := int64(-1)
		if st, statErr := os.Stat(logPath); statErr == nil {
			size = st.Size()
		}
		logger.Warnf("xcactivitylog unparsed at %s (xcode=%s size=%d): no CompilationCacheMetrics match", logPath, e.XcodeVersion, size)

		return 0, analytics.MetricsSourceLogUnparsed
	default:
		return 0, analytics.MetricsSourceLogUnparsed
	}
}

// pollLogFile is a bounded wait distinct from the correlator's retry bucket.
// Exponential backoff from 100ms until the log appears, stat surfaces a
// non-ENOENT error (ReadError), or the deadline passes (FileMissing).
//
// TODO: measure manifest -> log gap on real builds; collapse to one
// Sleep(100ms)+stat if gap is reliably <100ms.
func (e *Enricher) pollLogFile(path string, deadline time.Time) xcactivitylog.Outcome {
	logger := logOr(e.Logger)
	backoff := 100 * time.Millisecond

	for {
		_, err := os.Stat(path)
		switch {
		case err == nil:
			return xcactivitylog.OutcomeOK
		case !errors.Is(err, fs.ErrNotExist):
			logger.Warnf("xcactivitylog stat failed for %s: %s", path, err)

			return xcactivitylog.OutcomeReadError
		}

		if !e.now().Before(deadline) {
			return xcactivitylog.OutcomeFileMissing
		}

		time.Sleep(backoff)

		backoff *= 2
		if backoff > 500*time.Millisecond {
			backoff = 500 * time.Millisecond
		}
	}
}
