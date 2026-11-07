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

	// PayloadStore is the single-ID merge ledger. When non-nil, every
	// enrichment signal (sidecar lookup, manifest entry, xcactivitylog metric,
	// xcresult summary) is upserted into the store keyed by the proxy-derived
	// InvocationID before the PUT runs. Final PUTs mark the record as emitted
	// so the crash-fallback sweep knows to skip them.
	PayloadStore *PayloadStore

	// OnEmit fires after a successful PutInvocation with the ID actually sent
	// so the Watcher can mark it in the per-tick dedup set.
	OnEmit func(invocationID string)

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

// resolveSidecar runs the SidecarReader with a LookupFilter whose DerivedData
// disambiguates parallel builds sharing a time window; the axis degrades
// gracefully when either source is empty.
func (e *Enricher) resolveSidecar(manifestPath string, group ManifestEntryGroup) (*blobstats.Snapshot, []string, string, bool, bool) {
	if e.SidecarReader == nil {
		return nil, nil, "", false, false
	}

	filter := LookupFilter{
		DerivedData: pathsutil.DerivedDataFromManifestPath(manifestPath),
	}

	return e.SidecarReader.Lookup(group, filter)
}

// Enrich is the Watcher.Handle callback. manifestPath anchors the sibling
// xcactivitylog read used to populate the wrapperless hit rate.
//
// Returns true when the group was deferred (a matching sidecar is still
// non-terminal and more CAS traffic may land under the same InvocationID);
// handleGroup leaves the UUID unmarked so the next tick re-enters this path
// and emits once every sidecar is terminal.
func (e *Enricher) Enrich(manifestPath string, group ManifestEntryGroup) bool {
	logger := logOr(e.Logger)

	if len(group.Entries) == 0 {
		return false
	}

	command := group.Command()
	if command == "" {
		logger.Debugf("Enrichment PUT skipped for group scheme=%q: side-effect only (uuids=%v)", group.SchemeName(), group.UUIDs())

		return false
	}

	hitRate, hitRateOutcome := aggregateHitRate(manifestPath, group, logger, e.metricsLookup)

	var (
		sidecarBlob         *blobstats.Snapshot
		consumedPaths       []string
		sidecarInvocationID string
		sidecarPending      bool
		sidecarFound        bool
	)
	sidecarBlob, consumedPaths, sidecarInvocationID, sidecarPending, sidecarFound = e.resolveSidecar(manifestPath, group)

	if sidecarPending {
		logger.Debugf("Enrichment PUT deferred for scheme=%q cmd=%q: sidecar still non-terminal (uuids=%v)", group.SchemeName(), command, group.UUIDs())

		return true
	}

	invocationID := sidecarInvocationID
	if invocationID == "" {
		invocationID = group.ManifestUUID()
	}
	if invocationID == "" {
		logger.Warnf("Enrichment PUT skipped for scheme=%q cmd=%q: no InvocationID source (sidecar / manifest both empty)", group.SchemeName(), command)

		return false
	}

	// Sidecar-sourced groups carry their own data path — the xcactivitylog is
	// only the authoritative source for real manifest-derived emissions.
	if hitRateOutcome != xcactivitylog.OutcomeOK && !sidecarFound {
		logger.Debugf("Enrichment PUT skipped for group scheme=%q: xcactivitylog outcome=%v and no sidecar (uuids=%v)", group.SchemeName(), hitRateOutcome, group.UUIDs())

		return false
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

	e.upsertPayloadStore(invocationID, group, runStats, hitRateOutcome, sidecarBlob, PayloadStatusPendingTerminal)

	if err := e.Client.PutInvocation(*inv); err != nil {
		logger.Warnf("Failed to PUT enriched invocation %s: %s", invocationID, err)
		TickFailure(e.Health, e.Logger, e.now(), err)
		if persisted := e.recordWrapperlessFailure(invocationID, inv, err); persisted {
			unlinkSidecars(consumedPaths, logger)
		}

		return false
	}

	e.upsertPayloadStore(invocationID, group, runStats, hitRateOutcome, sidecarBlob, PayloadStatusEmitted)
	unlinkSidecars(consumedPaths, logger)

	TickSuccess(e.Health, e.Logger, e.now(), false)

	if e.OnEmit != nil {
		e.OnEmit(invocationID)
	}

	logger.Infof("Enriched invocation PUT %s (wrapperless scheme=%s cmd=%s entries=%d)", invocationID, group.SchemeName(), command, len(group.Entries))
	logger.Infof("Invocation saved. Visit 👉 %s", VisitURL(invocationID))

	e.appendLocalLog(invocationID, command, group, runStats, inv)

	return false
}

// VisitURL is the invocation-detail URL rendered in the Visit line + the
// stop-proxy stdout summary.
func VisitURL(invocationID string) string {
	return "https://app.bitrise.io/build-cache/invocations/xcode/" + invocationID
}

// EmitOrphan is the single-ID pipeline's orphan-sidecar PUT. The Watcher
// hands in a *ProxyIDGroup built by GroupSidecarsByProxyID; EmitOrphan builds
// a minimal analytics.Invocation from the aggregated signals, upserts the
// payload store as PendingTerminal, PUTs under the proxy InvocationID, and
// upserts Emitted on success. Sidecar paths are unlinked after a successful
// PUT. Silent no-op when Client is nil (test fixtures).
func (e *Enricher) EmitOrphan(group *ProxyIDGroup) {
	if e == nil || group == nil || e.Client == nil || group.InvocationID == "" {
		return
	}

	logger := logOr(e.Logger)

	command := labelForRowType(group.Primary, group.DerivedDataPath)
	merged := MergedPayload{
		Blob:         group.Blob,
		Scheme:       schemeFromDerivedData(group.DerivedDataPath),
		Command:      command,
		Start:        group.Start,
		Stop:         group.Stop,
		DurationMs:   durationMs(group.Start, group.Stop),
		OrphanOrigin: true,
	}

	e.upsertPayload(group.InvocationID, merged, PayloadStatusPendingTerminal)

	inv := merged.ToInvocation(group.InvocationID, e.Auth, e.Metadata, e.XcodeVersion, e.XcodeBuildNumber)
	if err := e.Client.PutInvocation(inv); err != nil {
		logger.Warnf("Failed to PUT orphan invocation %s: %s", group.InvocationID, err)

		return
	}

	e.upsertPayload(group.InvocationID, merged, PayloadStatusEmitted)
	unlinkSidecars(group.SidecarPaths, logger)

	if e.OnEmit != nil {
		e.OnEmit(group.InvocationID)
	}

	logger.Infof("Enriched orphan PUT %s (wrapperless rowtype=%s cmd=%s sidecars=%d)",
		group.InvocationID, group.Primary, command, len(group.SidecarPaths))
	logger.Infof("Invocation saved. Visit 👉 %s", VisitURL(group.InvocationID))
}

// labelForRowType maps a classified RowType to the `command` string the BE
// row carries. Scheme suffix comes from the DerivedData basename when
// available; otherwise the bare label travels alone.
func labelForRowType(rt RowType, derivedDataPath string) string {
	scheme := schemeFromDerivedData(derivedDataPath)
	base := map[RowType]string{
		RowMainBuild:      "build",
		RowDarkBuild:      "build",
		RowIndexer:        "indexing",
		RowPackageResolve: "resolve-packages",
		RowSigning:        "signing",
		RowLocalization:   "localization",
		RowClean:          "clean",
		RowArchive:        "archive",
		RowTest:           "test",
		RowInstallHeaders: "install-headers",
		// Keep the unclassified marker visible in BE rows so a classifier
		// regression surfaces instead of masquerading as "build".
		RowUnclassified: "unclassified",
	}
	label, ok := base[rt]
	if !ok {
		label = "unclassified"
	}

	if scheme == "" {
		return label
	}

	return label + " [" + scheme + "]"
}

// schemeFromDerivedData parses the <scheme>-<hash> DerivedData basename into
// a scheme name. Returns "" when the DD path does not match the
// <scheme>-<hash> shape (direct `xcodebuild -derivedDataPath ./foo` lands as
// "foo", which carries no scheme hint).
func schemeFromDerivedData(dd string) string {
	if dd == "" {
		return ""
	}

	base := filepath.Base(dd)
	// Xcode's layout is <SchemeOrWorkspace>-<alphanumHash>. Trim the trailing
	// -<hash> if present.
	if idx := strings.LastIndex(base, "-"); idx > 0 {
		candidate := base[idx+1:]
		if len(candidate) >= 20 && isAlnum(candidate) {
			return base[:idx]
		}
	}

	return base
}

func isAlnum(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}

	return true
}

func durationMs(start, stop time.Time) int64 {
	if start.IsZero() || stop.IsZero() || !stop.After(start) {
		return 0
	}

	return stop.Sub(start).Milliseconds()
}

// upsertPayload writes a PayloadRecord for any caller that already has a
// built MergedPayload. Silent no-op when PayloadStore is nil (test fixtures
// that only observe the PUT).
func (e *Enricher) upsertPayload(invocationID string, merged MergedPayload, status PayloadStatus) {
	if e.PayloadStore == nil {
		return
	}

	rec := PayloadRecord{
		InvocationID:  invocationID,
		LastUpdatedAt: e.now(),
		Status:        status,
		Merged:        merged,
	}

	if err := e.PayloadStore.Upsert(rec); err != nil {
		logOr(e.Logger).Debugf("payload store upsert %s status=%s failed: %s", invocationID, status, err)
	}
}

// EmitCrashFallback re-emits a PayloadStore record whose crash-fallback timer
// fired — a crash must be reported, not silently dropped. Reads the last-known
// merged payload from the ledger and PUTs it; a successful PUT marks the
// record Emitted (cancels any future timer); a failed PUT leaves it at
// PendingTerminal for the next scan. Silent no-op when any dependency is nil
// or the record is already Emitted.
func (e *Enricher) EmitCrashFallback(invocationID string) {
	if e == nil || e.PayloadStore == nil || e.Client == nil || invocationID == "" {
		return
	}

	logger := logOr(e.Logger)

	records, err := e.PayloadStore.Load()
	if err != nil {
		logger.Debugf("crash-fallback load payload store for %s failed: %s", invocationID, err)

		return
	}

	var rec PayloadRecord
	found := false
	for _, r := range records {
		if r.InvocationID == invocationID {
			rec = r
			found = true

			break
		}
	}

	if !found {
		logger.Debugf("crash-fallback: no payload record for %s — nothing to re-emit", invocationID)

		return
	}

	if rec.Status == PayloadStatusEmitted {
		return
	}

	inv := rec.Merged.ToInvocation(invocationID, e.Auth, e.Metadata, e.XcodeVersion, e.XcodeBuildNumber)

	if err := e.Client.PutInvocation(inv); err != nil {
		logger.Warnf("crash-fallback PUT %s failed: %s", invocationID, err)

		return
	}

	if err := e.PayloadStore.Upsert(PayloadRecord{
		InvocationID:  invocationID,
		LastUpdatedAt: e.now(),
		Status:        PayloadStatusEmitted,
		Merged:        rec.Merged,
	}); err != nil {
		logger.Debugf("crash-fallback upsert Emitted for %s failed: %s", invocationID, err)
	}

	logger.Infof("Crash-fallback PUT %s (no terminal signal within window)", invocationID)
	logger.Infof("Invocation saved. Visit 👉 %s", VisitURL(invocationID))

	if e.OnEmit != nil {
		e.OnEmit(invocationID)
	}
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

// outcomeLabel maps an xcactivitylog.Outcome to a stable string label for
// the payload store so analytics can key on it without depending on the int
// representation.
func outcomeLabel(o xcactivitylog.Outcome) string {
	switch o {
	case xcactivitylog.OutcomeOK:
		return "ok"
	case xcactivitylog.OutcomeFileMissing:
		return "file_missing"
	case xcactivitylog.OutcomeEmpty:
		return "empty"
	case xcactivitylog.OutcomeUnparsed:
		return "unparsed"
	case xcactivitylog.OutcomeReadError:
		return "read_error"
	default:
		return "unknown"
	}
}

// upsertPayloadStore merges the current signals into the single-ID ledger
// under invocationID. Called twice per emission: once before PUT (status =
// PendingTerminal) and once after a successful PUT (status = Emitted). A nil
// store is a silent no-op.
func (e *Enricher) upsertPayloadStore(
	invocationID string,
	group ManifestEntryGroup,
	runStats analytics.InvocationRunStats,
	logOutcome xcactivitylog.Outcome,
	sidecarBlob *blobstats.Snapshot,
	status PayloadStatus,
) {
	if e == nil || e.PayloadStore == nil || invocationID == "" {
		return
	}

	merged := MergedPayload{
		Blob:         sidecarBlob,
		HitRate:      runStats.HitRate,
		HitRateKnown: logOutcome == xcactivitylog.OutcomeOK,
		Scheme:       group.SchemeName(),
		Command:      runStats.Command,
		FullCommand:  runStats.FullCommand,
		Start:        group.Start(),
		Stop:         group.Stop(),
		DurationMs:   runStats.Duration,
		LogOutcome:   outcomeLabel(logOutcome),
		SuccessKnown: true,
		Success:      runStats.Success,
	}
	if runStats.Error != nil {
		merged.ErrorMessage = runStats.Error.Error()
	}

	e.upsertPayload(invocationID, merged, status)
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
// that carries a FileName and a known action. Groups hold one entry today; the
// sum form survives if grouping gains multi-entry shapes again. The lookup is
// the Watcher's per-tick cache so a log is gunzipped at most once per scan;
// nil falls through to a direct read.
//
// Outcome is OutcomeOK when at least one entry parsed; otherwise returns the
// last non-OK outcome so the existing gate in Enrich behaves as before.
func aggregateHitRate(manifestPath string, group ManifestEntryGroup, logger log.Logger, lookup MetricsLookup) (float32, xcactivitylog.Outcome) {
	if manifestPath == "" {
		return 0, xcactivitylog.OutcomeFileMissing
	}

	var (
		sumHits  int
		sumTotal int
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
	}

	if !sawAny {
		return 0, xcactivitylog.OutcomeFileMissing
	}

	if !anyOK {
		return 0, lastBad
	}

	var rate float32
	if sumTotal > 0 {
		rate = float32(sumHits) / float32(sumTotal)
	}

	return rate, xcactivitylog.OutcomeOK
}
