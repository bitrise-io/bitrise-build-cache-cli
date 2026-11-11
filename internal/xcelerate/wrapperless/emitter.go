package wrapperless

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/invocationlog"
)

//go:generate moq -stub -out invocation_putter_mock_test.go -pkg wrapperless_test . InvocationPutter

type InvocationPutter interface {
	PutInvocation(inv analytics.Invocation) error
}

type Phase string

const (
	PhaseSlim     Phase = "slim"
	PhaseEnriched Phase = "enriched"
)

const (
	defaultDrainInterval = 30 * time.Second
	defaultMaxPendingAge = 30 * 24 * time.Hour
)

// Payload is the wrapperless emitter's single row representation; Phase
// distinguishes the two PUT points for the same InvocationID. PersistedAt
// drives the age-out gate inside the drain loop.
type Payload struct {
	Phase       Phase     `json:"phase"`
	Bucket      Bucket    `json:"bucket"`
	HitRate     float32   `json:"hitRate"`
	PersistedAt time.Time `json:"persistedAt,omitempty"`
}

// Emitter owns the two-phase PUT path. Slim arms the row the moment a Start
// signal is observed; Enriched updates it with the matched End + CAS stats.
// Failed PUTs land on NDJSON ledgers (one per phase) and are replayed by a
// background drain goroutine started via Start().
type Emitter struct {
	Client              InvocationPutter
	Auth                auth.Credential
	Metadata            configcommon.CacheConfigMetadata
	XcodeVersion        string
	XcodeBuildNumber    string
	Logger              log.Logger
	PendingSlimPath     string
	PendingEnrichedPath string
	DrainInterval       time.Duration
	MaxPendingAge       time.Duration
	Now                 func() time.Time

	// Health, when non-nil, ticks around the enriched PUT (nil disables).
	Health *enrichment.HealthWriter

	// LocalLogAppender appends a Record on each successful enriched PUT so
	// `bitrise-build-cache invocations list` surfaces the wrapperless row.
	// nil disables.
	LocalLogAppender invocationlog.Appender

	mu sync.Mutex

	drainMu     sync.Mutex
	drainCancel context.CancelFunc
	drainDone   chan struct{}
}

func (e *Emitter) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}

	return time.Now()
}

func (e *Emitter) EmitSlim(bucket Bucket) {
	if e == nil || e.Client == nil {
		return
	}

	inv := e.buildInvocation(bucket, PhaseSlim, 0)

	if err := e.Client.PutInvocation(inv); err != nil {
		e.logWarn("wrapperless slim PUT %s failed: %s", inv.InvocationID, err)
		e.persistPending(Payload{Phase: PhaseSlim, Bucket: bucket})

		return
	}

	e.logDebug("Emitter: slim PUT id=%s scheme=%s", inv.InvocationID, bucket.Scheme)
}

func (e *Emitter) EmitEnriched(bucket Bucket, hitRate float32) {
	if e == nil || e.Client == nil {
		return
	}

	inv := e.buildInvocation(bucket, PhaseEnriched, hitRate)

	enrichment.TickAttempt(e.Health, e.Logger, e.now())

	if err := e.Client.PutInvocation(inv); err != nil {
		e.logWarn("wrapperless enriched PUT %s failed: %s", inv.InvocationID, err)
		enrichment.TickFailure(e.Health, e.Logger, e.now(), err)
		e.persistPending(Payload{Phase: PhaseEnriched, Bucket: bucket, HitRate: hitRate})

		return
	}

	enrichment.TickSuccess(e.Health, e.Logger, e.now(), true)

	e.appendLocalLog(bucket, inv, hitRate)

	e.logDebug("Emitter: enriched PUT id=%s scheme=%s bytes=%d hitRate=%.2f",
		inv.InvocationID, bucket.Scheme, snapshotBytes(bucket.BlobStats), hitRate)
}

// Start kicks off the background drain goroutine that replays entries from
// pending-slim.ndjson and pending-enriched.ndjson at DrainInterval cadence.
// Entries older than MaxPendingAge are dropped silently. Safe to call
// multiple times; subsequent calls after Start are no-ops until Stop.
func (e *Emitter) Start(ctx context.Context) {
	e.drainMu.Lock()
	defer e.drainMu.Unlock()

	if e.drainCancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	e.drainCancel = cancel
	e.drainDone = make(chan struct{})

	go e.drainLoop(ctx)
}

func (e *Emitter) Stop() {
	e.drainMu.Lock()
	cancel := e.drainCancel
	done := e.drainDone
	e.drainCancel = nil
	e.drainDone = nil
	e.drainMu.Unlock()

	if cancel != nil {
		cancel()
	}

	if done != nil {
		<-done
	}
}

func (e *Emitter) drainLoop(ctx context.Context) {
	defer close(e.drainDone)

	interval := e.DrainInterval
	if interval <= 0 {
		interval = defaultDrainInterval
	}

	// Opportunistic drain on start; catches whatever a prior proxy left behind.
	e.drainPending(PhaseSlim)
	e.drainPending(PhaseEnriched)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.drainPending(PhaseSlim)
			e.drainPending(PhaseEnriched)
		}
	}
}

func (e *Emitter) pendingPath(phase Phase) string {
	switch phase {
	case PhaseSlim:
		return e.PendingSlimPath
	case PhaseEnriched:
		return e.PendingEnrichedPath
	}

	return ""
}

func (e *Emitter) drainPending(phase Phase) {
	path := e.pendingPath(phase)
	if path == "" {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	f, err := os.Open(path) //nolint:gosec // app-owned state file
	if err != nil {
		if !os.IsNotExist(err) {
			e.logWarn("wrapperless drain open %s: %s", path, err)
		}

		return
	}

	maxAge := e.MaxPendingAge
	if maxAge <= 0 {
		maxAge = defaultMaxPendingAge
	}
	cutoff := e.now().Add(-maxAge)

	var kept []Payload
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var p Payload
		if err := json.Unmarshal(line, &p); err != nil {
			e.logWarn("wrapperless drain decode %s: %s", path, err)

			continue
		}

		if !p.PersistedAt.IsZero() && p.PersistedAt.Before(cutoff) {
			e.logDebug("Emitter: drain aged out phase=%s id=%s", p.Phase, p.Bucket.InvocationID)

			continue
		}

		inv := e.buildInvocation(p.Bucket, p.Phase, p.HitRate)
		if err := e.Client.PutInvocation(inv); err != nil {
			kept = append(kept, p)

			continue
		}

		e.logDebug("Emitter: drained pending phase=%s id=%s", p.Phase, inv.InvocationID)

		if p.Phase == PhaseEnriched {
			e.appendLocalLog(p.Bucket, inv, p.HitRate)
		}
	}
	_ = f.Close()

	if err := scanner.Err(); err != nil {
		e.logWarn("wrapperless drain scan %s: %s", path, err)
	}

	if len(kept) == 0 {
		_ = os.Remove(path)

		return
	}

	e.rewritePending(path, kept)
}

func (e *Emitter) rewritePending(path string, kept []Payload) {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // app-owned state file
	if err != nil {
		e.logWarn("wrapperless drain rewrite open %s: %s", tmp, err)

		return
	}
	defer f.Close() //nolint:errcheck

	for _, p := range kept {
		body, err := json.Marshal(p)
		if err != nil {
			e.logWarn("wrapperless drain rewrite marshal: %s", err)

			continue
		}
		if _, err := fmt.Fprintln(f, string(body)); err != nil {
			e.logWarn("wrapperless drain rewrite write %s: %s", tmp, err)

			return
		}
	}

	if err := os.Rename(tmp, path); err != nil {
		e.logWarn("wrapperless drain rewrite rename %s: %s", path, err)
	}
}

func (e *Emitter) appendLocalLog(bucket Bucket, inv analytics.Invocation, hitRate float32) {
	if e.LocalLogAppender == nil {
		return
	}

	rec := invocations.Record{
		InvocationID: inv.InvocationID,
		Command:      shortCommand(bucket),
		Tool:         invocations.ToolXcode,
		ToolVersion:  e.XcodeVersion,
		CLIVersion:   inv.CLIVersion,
		StartedAt:    bucket.StartAt.UTC(),
		FinishedAt:   bucket.EndAt.UTC(),
		ExitCode:     exitCodeFromBucket(bucket),
		CIProvider:   e.Metadata.CIProvider,
		Username:     inv.Username,
		HitRate:      hitRate,
	}

	if err := e.LocalLogAppender.Append(rec); err != nil {
		e.logWarn("wrapperless local log append %s failed: %s", inv.InvocationID, err)
	}
}

func exitCodeFromBucket(bucket Bucket) int {
	if bucket.SuccessKnown && !bucket.Success {
		return 1
	}

	return 0
}

func (e *Emitter) buildInvocation(bucket Bucket, phase Phase, hitRate float32) analytics.Invocation {
	now := e.now()

	start := bucket.StartAt
	if start.IsZero() {
		start = now
	}

	run := analytics.InvocationRunStats{
		InvocationDate:   start,
		InvocationID:     bucket.InvocationID,
		Duration:         bucket.Duration.Milliseconds(),
		Command:          shortCommand(bucket),
		FullCommand:      fullCommand(bucket),
		XcodeVersion:     e.XcodeVersion,
		XcodeBuildNumber: e.XcodeBuildNumber,
	}

	if phase == PhaseEnriched {
		run.HitRate = hitRate
		run.Success = bucket.Success
		run.CacheBlobStats = bucket.BlobStats
		if !bucket.Success && bucket.SuccessKnown {
			run.Error = errors.New("xcode build failed")
		}
	}

	inv := analytics.NewInvocation(run, e.Auth, e.Metadata)

	return *inv
}

// shortCommand renders the BE row's command string. Dedicated reasons
// (IDE-background, CLI-implicit) carry a fixed label so the BE row never
// masquerades as a user build; otherwise it's "<action> [<scheme> / <config>]"
// matching the wrapper flow's convention. Configuration is appended only when
// present; scheme-only renders as "<action> [<scheme>]".
func shortCommand(bucket Bucket) string {
	if label, ok := fixedReasonLabel(bucket.Reason); ok {
		return label
	}

	action := actionVerb(bucket)
	if bucket.Scheme == "" {
		return action
	}

	target := bucket.Scheme
	if bucket.Configuration != "" {
		target += " / " + bucket.Configuration
	}

	return action + " [" + target + "]"
}

// fixedReasonLabel returns the dedicated label for BucketReasons whose row
// must not render as a user build. The ok=false branch covers every
// user-action reason (End-matched, EndOfPrior, Unknown) which falls through
// to the default <action> [<scheme>] shape.
func fixedReasonLabel(r BucketReason) (string, bool) {
	switch r {
	case BucketReasonIDEBackground:
		return "ide background", true
	case BucketReasonCLIImplicit:
		return "cli run", true
	case BucketReasonUnknown, BucketReasonEndMatched, BucketReasonEndOfPrior, BucketReasonSpeculative:
	}

	return "", false
}

// fullCommand renders an argv-shaped xcodebuild invocation when enough
// metadata was captured from the LogStoreManifest signature; falls back to
// shortCommand when it would be a bare verb.
func fullCommand(bucket Bucket) string {
	if _, ok := fixedReasonLabel(bucket.Reason); ok {
		return shortCommand(bucket)
	}

	if bucket.Scheme == "" && bucket.Workspace == "" && bucket.Project == "" {
		return shortCommand(bucket)
	}

	parts := []string{"xcodebuild", actionVerb(bucket)}

	switch {
	case bucket.Workspace != "":
		parts = append(parts, "-workspace", bucket.Workspace)
	case bucket.Project != "":
		parts = append(parts, "-project", bucket.Project)
	}

	if bucket.Scheme != "" {
		parts = append(parts, "-scheme", bucket.Scheme)
	}

	if bucket.Configuration != "" {
		parts = append(parts, "-configuration", bucket.Configuration)
	}

	return strings.Join(parts, " ")
}

func actionVerb(bucket Bucket) string {
	if a := string(bucket.Action); a != "" {
		return a
	}

	return "build"
}

func (e *Emitter) persistPending(p Payload) {
	path := e.pendingPath(p.Phase)
	if path == "" {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.logWarn("wrapperless persist mkdir %s: %s", filepath.Dir(path), err)

		return
	}

	p.PersistedAt = e.now()

	body, err := json.Marshal(p)
	if err != nil {
		e.logWarn("wrapperless persist marshal phase=%s: %s", p.Phase, err)

		return
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // app-owned state file
	if err != nil {
		e.logWarn("wrapperless persist open %s: %s", path, err)

		return
	}
	defer f.Close() //nolint:errcheck

	if _, err := fmt.Fprintln(f, string(body)); err != nil {
		e.logWarn("wrapperless persist write %s: %s", path, err)

		return
	}

	e.logDebug("Emitter: persisted pending phase=%s id=%s", p.Phase, p.Bucket.InvocationID)
}

func (e *Emitter) logWarn(format string, args ...any) {
	if e.Logger == nil {
		return
	}

	e.Logger.Warnf(format, args...)
}

func (e *Emitter) logDebug(format string, args ...any) {
	if e.Logger == nil {
		return
	}

	e.Logger.Debugf(format, args...)
}
