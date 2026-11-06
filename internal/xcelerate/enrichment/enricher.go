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

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	pathsutil "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/invocationlog"
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
	XcresultParser   xcresult.Parser
	SidecarReader    SidecarReader

	// OnEmit fires after a successful PutInvocation with the ID actually sent,
	// the manifest's build-description hash (empty if not extractable), the
	// group whose PUT just succeeded, and the master's sidecar blob (nil when
	// the master had no sidecar / no CAS traffic). The watcher wires this to
	// its per-tick dedup set so an aged orphan sidecar sharing the ID with a
	// manifest emission does not re-emit as a synthetic pseudo-group — and so
	// the orphan sweep can bridge a pseudo-group back to the originating
	// manifest via hex-exact match on buildDescriptionHash. The master blob
	// is stashed so the S7 boundary-bridge clone can sum it with the orphan's
	// own snapshot (warm rebuilds carry traffic on both sides).
	OnEmit func(invocationID string, buildDescriptionHash string, group ManifestEntryGroup, masterBlob *blobstats.Snapshot)

	// LocalLogAppender appends a Record on each successful wrapperless PUT so
	// `bitrise-build-cache invocations list` surfaces the row. nil disables.
	LocalLogAppender invocationlog.Appender

	// metricsLookup is the per-tick xcactivitylog cache the Watcher installs
	// via SetMetricsLookup before invoking Enrich; aggregateHitRate consults
	// it so a single gunzip is shared with the grouping pass. nil = fall
	// through to a direct ReadMetricsFromManifest read.
	metricsLookup MetricsLookup
}

// SetMetricsLookup wires the Watcher's per-tick metrics cache. Called at scan
// start; cleared at scan tail so a stale cache from one tick cannot leak into
// the next.
func (e *Enricher) SetMetricsLookup(lookup MetricsLookup) {
	e.metricsLookup = lookup
}

func (e *Enricher) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}

	return time.Now()
}

// resolveSidecar returns the sidecar blob + consumed paths + derived InvocationID
// + found flag. Preattached groups short-circuit the time-overlap SidecarReader
// lookup (boundary-bridge path keeps the manifest UUID authoritative). Otherwise
// the SidecarReader runs with a LookupFilter whose DerivedData + build-description
// hash disambiguate parallel builds sharing a time window; both axes degrade
// gracefully when either source is empty.
func (e *Enricher) resolveSidecar(manifestPath string, group ManifestEntryGroup, buildDescriptionHash string) (*blobstats.Snapshot, []string, string, bool) {
	if blob, paths, ok := group.Preattached(); ok {
		return blob, paths, "", blob != nil
	}
	if e.SidecarReader == nil {
		return nil, nil, "", false
	}

	filter := LookupFilter{
		DerivedData: pathsutil.DerivedDataFromManifestPath(manifestPath),
	}
	if buildDescriptionHash != "" {
		filter.Hashes = []string{buildDescriptionHash}
	}

	return e.SidecarReader.Lookup(group, filter)
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

	hitRate, hitRateOutcome, buildDescriptionHash := aggregateHitRate(manifestPath, group, logger, e.metricsLookup)

	var (
		sidecarBlob         *blobstats.Snapshot
		consumedPaths       []string
		sidecarInvocationID string
		sidecarFound        bool
	)
	sidecarBlob, consumedPaths, sidecarInvocationID, sidecarFound = e.resolveSidecar(manifestPath, group, buildDescriptionHash)

	invocationID := group.ManifestUUID()
	if invocationID == "" {
		invocationID = group.OverrideInvocationID()
	}
	if invocationID == "" {
		invocationID = sidecarInvocationID
	}
	if invocationID == "" {
		logger.Warnf("Enrichment PUT skipped for scheme=%q cmd=%q: no InvocationID source (manifest / override / sidecar all empty)", group.SchemeName(), command)

		return
	}

	// Synthetic (sidecar-sourced) groups and override-anchored PUTs carry their
	// own data path — the xcactivitylog is only the authoritative source for
	// real manifest-derived emissions.
	hasAlternateDataSource := sidecarFound || group.OverrideInvocationID() != ""
	if hitRateOutcome != xcactivitylog.OutcomeOK && !hasAlternateDataSource {
		logger.Debugf("Enrichment PUT skipped for group scheme=%q: xcactivitylog outcome=%v and no sidecar / override (uuids=%v)", group.SchemeName(), hitRateOutcome, group.UUIDs())

		return
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

	if e.OnEmit != nil {
		e.OnEmit(invocationID, buildDescriptionHash, group, sidecarBlob)
	}

	logger.Infof("Enriched invocation PUT %s (wrapperless scheme=%s cmd=%s entries=%d)", invocationID, group.SchemeName(), command, len(group.Entries))
	logger.Infof("Invocation saved. Visit 👉 %s", VisitURL(invocationID))

	e.appendLocalLog(invocationID, command, group, runStats, inv)
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
		logger.Warnf("Failed to append orphan retry record %s: %s", invocationID, err)

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

// attachXcresultSummary merges xcresult bundles across every manifest entry
// with a non-empty FileName. Each entry owns its own sibling bundle; a
// clean+build+test cluster writes three bundles that must fuse into one BE row.
// Targets dedup on Name (take the longest BuildDurationMs so a test action
// does not overwrite the build action's measurement); Failures dedup on the
// (TargetName, Message) pair. Silent no-op when parser is unset or every
// bundle is absent/empty.
func (e *Enricher) attachXcresultSummary(manifestPath string, group ManifestEntryGroup, inv *analytics.Invocation) {
	if e.XcresultParser == nil || manifestPath == "" {
		return
	}

	manifestDir := filepath.Dir(manifestPath)
	seenTargets := make(map[string]int, len(group.Entries))
	seenFailures := make(map[string]struct{}, len(group.Entries))

	for _, entry := range group.Entries {
		if entry.FileName == "" {
			continue
		}

		bundlePath := filepath.Join(manifestDir, strings.TrimSuffix(entry.FileName, filepath.Ext(entry.FileName))+".xcresult")
		if _, err := os.Stat(bundlePath); err != nil {
			continue
		}

		summary := e.XcresultParser.Parse(context.Background(), bundlePath)

		for _, t := range summary.Targets {
			if idx, ok := seenTargets[t.Name]; ok {
				if t.BuildDurationMs > inv.Targets[idx].BuildDurationMs {
					inv.Targets[idx].BuildDurationMs = t.BuildDurationMs
				}

				continue
			}

			seenTargets[t.Name] = len(inv.Targets)
			inv.Targets = append(inv.Targets, analytics.TargetSummary{
				Name:            t.Name,
				BuildDurationMs: t.BuildDurationMs,
			})
		}
		for _, f := range summary.Failures {
			key := f.TargetName + "\x00" + f.Message
			if _, ok := seenFailures[key]; ok {
				continue
			}

			seenFailures[key] = struct{}{}
			inv.Failures = append(inv.Failures, analytics.FailureSummary{
				TargetName: f.TargetName,
				Message:    f.Message,
			})
		}
	}

	if len(inv.Targets) == 0 && len(inv.Failures) == 0 {
		return
	}

	logOr(e.Logger).Debugf("Attached xcresult summary to invocation %s (targets=%d, failures=%d)", inv.InvocationID, len(inv.Targets), len(inv.Failures))
}

// aggregateHitRate sums the compile-cache counters across every manifest entry
// that carries a FileName and a known action. Groups hold one entry post-S7;
// the sum form survives if grouping gains multi-entry shapes again. The lookup
// is the Watcher's per-tick cache so a log is gunzipped at most once per scan;
// nil falls through to a direct read.
//
// Outcome is OutcomeOK when at least one entry parsed; otherwise returns the
// last non-OK outcome so the existing gate in Enrich behaves as before.
// BuildDescriptionHash is sourced from the first OK log in iteration order.
func aggregateHitRate(manifestPath string, group ManifestEntryGroup, logger log.Logger, lookup MetricsLookup) (float32, xcactivitylog.Outcome, string) {
	if manifestPath == "" {
		return 0, xcactivitylog.OutcomeFileMissing, ""
	}

	var (
		sumHits  int
		sumTotal int
		hash     string
		anyOK    bool
		lastBad  = xcactivitylog.OutcomeFileMissing
		sawAny   bool
	)

	for _, entry := range group.Entries {
		if entry.FileName == "" || entry.Command() == CommandUnknown {
			continue
		}

		sawAny = true

		var m xcactivitylog.Metrics
		if lookup != nil {
			m = lookup(entry)
		} else {
			m = xcactivitylog.ReadMetricsFromManifest(manifestPath, entry.FileName, logger)
		}

		if m.Outcome != xcactivitylog.OutcomeOK {
			lastBad = m.Outcome

			continue
		}

		anyOK = true
		sumHits += m.Hits
		sumTotal += m.Total

		if hash == "" && m.BuildDescriptionHash != "" {
			hash = m.BuildDescriptionHash
		}
	}

	if !sawAny {
		return 0, xcactivitylog.OutcomeFileMissing, ""
	}

	if !anyOK {
		return 0, lastBad, ""
	}

	var rate float32
	if sumTotal > 0 {
		rate = float32(sumHits) / float32(sumTotal)
	}

	return rate, xcactivitylog.OutcomeOK, hash
}
