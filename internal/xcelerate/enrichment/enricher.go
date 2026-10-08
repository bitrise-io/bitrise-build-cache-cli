package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcresult"
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
	SidecarReader    SidecarReader
	XcresultParser   xcresult.Parser
	// LocalLogAppender appends a Record on each successful orphan PUT so
	// `bitrise-build-cache invocations list` surfaces the row. nil disables.
	LocalLogAppender LocalLogAppender
	// EmittedURLSink receives the invocation ID of every successful orphan PUT
	// so stop-proxy can print a Visit URL per orphan. nil disables.
	EmittedURLSink EmittedURLSink
}

// LocalLogAppender is the subset of invocations.Writer used by the orphan
// path. Defined here (instead of imported) to keep enricher's test harness
// free of the invocations package when callers inject fakes.
type LocalLogAppender interface {
	Append(rec invocations.Record) error
}

// EmittedURLSink receives each enriched invocation ID so stop-proxy can
// enumerate the orphan Visit URLs the current proxy emitted.
type EmittedURLSink interface {
	AppendEmittedURL(invocationID string) error
}

func (e *Enricher) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}

	return time.Now()
}

// Enrich is the Watcher.Handle callback. manifestPath anchors the sibling
// xcactivitylog read used to populate the wrapperless hit rate.
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

	hitRate, hitRateOutcome := e.readLogHitRate(manifestPath, group)

	var (
		sidecarBlob   *blobstats.Snapshot
		consumedPaths []string
		sidecarFound  bool
	)
	if e.SidecarReader != nil {
		sidecarBlob, consumedPaths, sidecarFound = e.SidecarReader.Lookup(group)
	}

	if sidecarFound && hitRateOutcome != xcactivitylog.OutcomeOK && sidecarBlob != nil {
		if total := sidecarBlob.Download.OpCount + sidecarBlob.Download.MissCount; total > 0 {
			hitRate = float32(sidecarBlob.Download.OpCount) / float32(total)
		}
	}

	var runErr error
	if !group.Success() {
		runErr = errors.New(group.ErrorMessage())
	}

	runStats := analytics.InvocationRunStats{
		InvocationDate:   group.Start(),
		InvocationID:     invocationID,
		Duration:         group.Duration().Milliseconds(),
		Command:          command,
		FullCommand:      group.FullCommand(),
		Success:          group.Success(),
		Error:            runErr,
		XcodeVersion:     e.XcodeVersion,
		XcodeBuildNumber: e.XcodeBuildNumber,
		HitRate:          hitRate,
		CacheBlobStats:   sidecarBlob,
	}

	inv := analytics.NewInvocation(runStats, e.Auth, e.Metadata)

	e.attachXcresultSummary(manifestPath, group, inv)

	TickAttempt(e.Health, e.Logger, e.now())

	if err := e.Client.PutInvocation(*inv); err != nil {
		logger.Warnf("Failed to PUT enriched invocation %s: %s", invocationID, err)
		TickFailure(e.Health, e.Logger, e.now(), err)
		if persisted := e.recordWrapperlessFailure(invocationID, inv, err); persisted {
			unlinkSidecars(consumedPaths, logger)
		}

		return
	}

	unlinkSidecars(consumedPaths, logger)

	// matched=false always: matched groups short-circuit above and LastMatched
	// is reserved for correlated re-PUTs, which no longer happen.
	TickSuccess(e.Health, e.Logger, e.now(), false)

	// Format consumed by internal/config/xcelerate/stop_proxy.go:enrichedPutRe; keep in sync.
	logger.Infof("Enriched invocation PUT %s (wrapperless scheme=%s cmd=%s entries=%d)", invocationID, group.SchemeName(), command, len(group.Entries))
	logger.Infof("Invocation saved. Visit 👉 %s", VisitURL(invocationID))

	e.appendLocalLog(invocationID, command, group, runStats, inv)
	e.appendEmittedURL(invocationID)
}

func (e *Enricher) appendEmittedURL(invocationID string) {
	if e.EmittedURLSink == nil {
		return
	}

	if err := e.EmittedURLSink.AppendEmittedURL(invocationID); err != nil {
		logOr(e.Logger).Warnf("Failed to append emitted URL for %s: %s", invocationID, err)
	}
}

// VisitURL is the invocation-detail URL rendered in the Visit line + the
// stop-proxy stdout summary.
func VisitURL(invocationID string) string {
	return "https://app.bitrise.io/build-cache/invocations/xcode/" + invocationID
}

// appendLocalLog surfaces the wrapperless row in `bitrise-build-cache invocations
// list`. Failures are warn-only — the analytics PUT is already durable.
func (e *Enricher) appendLocalLog(invocationID, command string, group ManifestEntryGroup, runStats analytics.InvocationRunStats, inv *analytics.Invocation) {
	if e.LocalLogAppender == nil {
		return
	}

	rec := invocations.Record{
		InvocationID: invocationID,
		Command:      command,
		Tool:         invocations.ToolXcode,
		ToolVersion:  e.XcodeVersion,
		CLIVersion:   inv.CLIVersion,
		StartedAt:    group.Start().UTC(),
		FinishedAt:   group.Stop().UTC(),
		ExitCode:     exitCodeFromRun(runStats),
		CIProvider:   e.Metadata.CIProvider,
		Username:     inv.Username,
		HitRate:      runStats.HitRate,
	}

	if err := e.LocalLogAppender.Append(rec); err != nil {
		logOr(e.Logger).Warnf("Failed to append wrapperless invocation to local log: %s", err)
	}
}

func exitCodeFromRun(runStats analytics.InvocationRunStats) int {
	if runStats.Success {
		return 0
	}

	return 1
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

// recordWrapperlessFailure returns true when the enriched payload was persisted to
// the retry store, so sidecar unlink can proceed; false when the record was
// dropped and sidecars must be left for a later invocation to re-consume.
func (e *Enricher) recordWrapperlessFailure(invocationID string, inv *analytics.Invocation, putErr error) bool {
	if e.Store == nil {
		return false
	}

	logger := logOr(e.Logger)

	payload, err := json.Marshal(inv)
	if err != nil {
		logger.Warnf("Failed to marshal enriched invocation %s for retry: %s", invocationID, err)

		return false
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
		logger.Warnf("Failed to append wrapperless retry record %s: %s", invocationID, err)

		return false
	}

	return true
}

func unlinkSidecars(paths []string, logger log.Logger) {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			logger.Warnf("Failed to remove consumed sidecar %s: %s", p, err)
		}
	}
}

// attachXcresultSummary populates inv.Targets/Failures from the xcresult bundle
// Xcode writes next to the activity log. Silent no-op when parser is unset,
// bundle is absent, or the summary is empty.
func (e *Enricher) attachXcresultSummary(manifestPath string, group ManifestEntryGroup, inv *analytics.Invocation) {
	if e.XcresultParser == nil {
		return
	}

	bundlePath := xcresultBundlePath(manifestPath, group)
	if bundlePath == "" {
		return
	}
	if _, err := os.Stat(bundlePath); err != nil {
		return
	}

	summary := e.XcresultParser.Parse(context.Background(), bundlePath)
	if len(summary.Targets) == 0 && len(summary.Failures) == 0 {
		return
	}

	for _, t := range summary.Targets {
		inv.Targets = append(inv.Targets, analytics.TargetSummary{
			Name:            t.Name,
			BuildDurationMs: t.BuildDurationMs,
		})
	}
	for _, f := range summary.Failures {
		inv.Failures = append(inv.Failures, analytics.FailureSummary{
			TargetName: f.TargetName,
			Message:    f.Message,
		})
	}

	logOr(e.Logger).Debugf("Attached xcresult summary to invocation %s (targets=%d, failures=%d)", inv.InvocationID, len(inv.Targets), len(inv.Failures))
}

// xcresultBundlePath derives the sibling xcresult bundle path from the manifest
// group's primary activity-log entry: same directory as the manifest, same
// basename as the .xcactivitylog with the extension swapped for .xcresult.
func xcresultBundlePath(manifestPath string, group ManifestEntryGroup) string {
	if manifestPath == "" {
		return ""
	}

	primary := group.Primary()
	if primary.FileName == "" {
		return ""
	}

	base := strings.TrimSuffix(primary.FileName, filepath.Ext(primary.FileName))

	return filepath.Join(filepath.Dir(manifestPath), base+".xcresult")
}

// readLogHitRate returns 0 on any non-OK outcome. The log lands on disk before
// the manifest (Xcode writes the manifest last), so no bounded wait is needed —
// the reader's ENOENT path handles the vanishing race.
func (e *Enricher) readLogHitRate(manifestPath string, group ManifestEntryGroup) (float32, xcactivitylog.Outcome) {
	logger := logOr(e.Logger)

	if manifestPath == "" {
		return 0, xcactivitylog.OutcomeFileMissing
	}

	primary := group.Primary()
	if primary.FileName == "" {
		return 0, xcactivitylog.OutcomeFileMissing
	}

	logPath := filepath.Join(filepath.Dir(manifestPath), primary.FileName)

	metrics, err := xcactivitylog.ReadCompilationCacheMetricsWithLogger(logPath, logger)
	if err != nil {
		logger.Warnf("xcactivitylog read failed for %s: %s", logPath, err)
	}

	switch metrics.Outcome {
	case xcactivitylog.OutcomeOK:
		return metrics.HitRate, metrics.Outcome
	case xcactivitylog.OutcomeFileMissing:
		logger.Debugf("xcactivitylog missing at %s", logPath)
	case xcactivitylog.OutcomeEmpty:
		logger.Debugf("xcactivitylog empty at %s", logPath)
	case xcactivitylog.OutcomeUnparsed:
		size := int64(-1)
		if st, statErr := os.Stat(logPath); statErr == nil {
			size = st.Size()
		}
		logger.Warnf("xcactivitylog unparsed at %s (xcode=%s size=%d): no CompilationCacheMetrics match", logPath, e.XcodeVersion, size)
	case xcactivitylog.OutcomeReadError:
	}

	return 0, metrics.Outcome
}
