package wrapperless

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildidentity"
)

// AnchorSaltIDEBackground is the fixed label slotted into the build-request
// field of the identity anchor for the IDE-background bucket. Not a real
// xcbuilddata hex — the value is a stable salt so trailing CAS traffic after
// a Quit{IDE} lands on its own BE row with a deterministic InvocationID.
const AnchorSaltIDEBackground = "ide-background"

// AnchorSaltCLIRun is the fixed label slotted into the build-request field of
// the identity anchor for the CLI-implicit bucket. Not a real xcbuilddata hex
// — the value is a stable salt so a Quit{CLI} with no End still flushes to a
// deterministic BE row.
const AnchorSaltCLIRun = "cli-run"

// anchorSaltSpecFmt derives per-cycle InvocationIDs for speculative buckets.
// Every bucket close opens a fresh spec so proxy traffic between builds (or
// during warm rebuilds that skip Start) lands under an ID that will be
// classified at the next observed signal.
const anchorSaltSpecFmt = "pending-%d"

// BlobStatsSource is the Allocator's windowed-stats seam. TakeSnapshot must
// snapshot and reset atomically so an RPC interleaving between the two does
// not land in both the current bucket and the next one.
type BlobStatsSource interface {
	TakeSnapshot() *blobstats.Snapshot
}

// intakeCapacity caps the multi-producer (DDWatcher + QuitWatcher) →
// single-consumer (Run) timeline channel. Overflow drops the newest event —
// the pipeline is a best-effort attributor, not an at-least-once queue.
const intakeCapacity = 10_000

type Allocator struct {
	Hostname        string
	ProxyPID        int
	ProxyStartMS    int64
	BlobStats       BlobStatsSource
	OnReady         func(Bucket)
	OnOpenInvoc     func(Bucket)
	Now             func() time.Time
	IDEBackgroundTO time.Duration
	Logger          log.Logger

	mu         sync.Mutex
	open       *Bucket
	done       chan struct{}
	ideTimerMu sync.Mutex
	ideTimer   *time.Timer

	intake     chan TimelineEvent
	intakeDone chan struct{}
	closeOnce  sync.Once

	// schemeEmittedSinceStart records whether a scheme-bearing End bucket was
	// emitted since the most recent Start. Used by flushCLI to suppress the
	// CLIImplicit fallback bucket whose only job is to catch a CLI invocation
	// that quit without ever sealing a manifest — if a real End already
	// emitted this cycle, the fallback is CAS tail noise, not a build.
	schemeEmittedSinceStart bool
}

func NewAllocator(hostname string, proxyPID int, proxyStartMS int64) *Allocator {
	return &Allocator{
		Hostname:     hostname,
		ProxyPID:     proxyPID,
		ProxyStartMS: proxyStartMS,
		intake:       make(chan TimelineEvent, intakeCapacity),
		intakeDone:   make(chan struct{}),
	}
}

func (a *Allocator) PushStart(s Start) { a.push(TimelineEvent{Kind: EventKindStart, Start: s}) }
func (a *Allocator) PushEnd(e End)     { a.push(TimelineEvent{Kind: EventKindEnd, End: e}) }
func (a *Allocator) PushQuit(q Quit)   { a.push(TimelineEvent{Kind: EventKindQuit, Quit: q}) }

func (a *Allocator) push(ev TimelineEvent) {
	select {
	case <-a.intakeDone:
		return
	default:
	}

	select {
	case a.intake <- ev:
	default:
	}
}

// CloseIntake signals Run to flush buffered events and return. Pipeline.Close
// orders this before cancelling ctx so a mid-flight End queued behind a slow
// enriched PUT is not silently dropped on shutdown. The data channel is
// intentionally not closed — doing so races with concurrent producers past
// their intakeDone check.
func (a *Allocator) CloseIntake() {
	a.closeOnce.Do(func() {
		close(a.intakeDone)
	})
}

func (a *Allocator) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}

	return time.Now()
}

// Handle routes a single timeline event into the state machine. Callers may
// drive the allocator synchronously (tests) or via Drain (pipeline).
func (a *Allocator) Handle(ev TimelineEvent) {
	switch ev.Kind {
	case EventKindStart:
		a.onStart(ev.Start)
	case EventKindEnd:
		a.onEnd(ev.End)
	case EventKindQuit:
		a.onQuit(ev.Quit)
	}
}

// Run drains the internal intake channel into Handle. On CloseIntake or ctx
// cancel it still consumes any already-buffered events before returning.
// Single-reader by design — Allocator's state machine is not safe for
// concurrent drivers.
func (a *Allocator) Run(ctx context.Context) {
	a.mu.Lock()
	if a.done == nil {
		a.done = make(chan struct{})
	}
	done := a.done
	a.mu.Unlock()

	defer close(done)

	for {
		select {
		case ev := <-a.intake:
			a.Handle(ev)
		case <-a.intakeDone:
			a.drainIntake()

			return
		case <-ctx.Done():
			a.drainIntake()

			return
		}
	}
}

func (a *Allocator) drainIntake() {
	for {
		select {
		case ev := <-a.intake:
			a.Handle(ev)
		default:
			return
		}
	}
}

// Init opens the first speculative bucket so proxy traffic observed before
// any Start has a live InvocationID to stamp under. Idempotent: callers may
// safely invoke it from Pipeline startup regardless of prior state.
func (a *Allocator) Init() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.open == nil {
		a.openSpec(a.now())
	}
}

func isSpec(b *Bucket) bool {
	return b != nil && b.Reason == BucketReasonSpeculative
}

// openSpec mints a fresh speculative bucket keyed on a monotonic salt and
// stamps the proxy. Reason=Unknown signals downstream (Pipeline.onBucketOpen)
// to skip the slim PUT — the row materialises only once the spec is
// classified at the next Start / End / Quit.
func (a *Allocator) openSpec(at time.Time) {
	salt := fmt.Sprintf(anchorSaltSpecFmt, at.UnixNano())
	anchor := buildidentity.Anchor{
		Hostname:        a.Hostname,
		PID:             a.ProxyPID,
		StartTimeMS:     a.ProxyStartMS,
		BuildRequestHex: salt,
	}

	spec := &Bucket{ //nolint:exhaustruct // other fields filled on classification
		InvocationID: buildidentity.Derive(anchor),
		StartAt:      at,
		Reason:       BucketReasonSpeculative,
		PeerPID:      a.ProxyPID,
		PeerStartMS:  a.ProxyStartMS,
	}
	a.open = spec

	a.debugf("Allocator: spec opened id=%s", spec.InvocationID)

	if a.OnOpenInvoc != nil {
		a.OnOpenInvoc(*spec)
	}
}

func (a *Allocator) onStart(s Start) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.cancelIDETimer()

	if isSpec(a.open) {
		spec := a.open
		a.open = nil
		spec.EndAt = s.At
		spec.BlobStats = a.snapshotBlobs()
		if hasActivity(spec.BlobStats) {
			spec.Reason = BucketReasonIDEBackground
			a.emit(spec)
		}
	} else if a.open != nil {
		// Single-slot design: a second concurrent Start evicts the prior as
		// EndOfPrior rather than opening a parallel bucket. Correct for serial
		// IDE ⌘B and sequential CLI runs; parallel CLI builds from the same
		// shell get interleaved attribution — proxy's global CurrentBucketID
		// stamps every RPC with the latest, so the prior bucket closes with
		// whatever bytes it saw before the swap.
		prior := a.open
		a.warnf("Allocator: concurrent Start evicts bucket id=%s prior_hex=%s next_hex=%s", prior.InvocationID, prior.BuildRequestHex, s.BuildRequestHex)
		prior.EndAt = s.At
		prior.Reason = BucketReasonEndOfPrior
		prior.BlobStats = a.snapshotBlobs()
		a.open = nil
		a.emit(prior)
	}

	fresh := a.newBucket(s.At, s)
	a.open = fresh
	a.schemeEmittedSinceStart = false

	a.infof("Allocator: bucket opened id=%s action=%s", fresh.InvocationID, fresh.Action)

	if a.OnOpenInvoc != nil && fresh.InvocationID != "" {
		a.OnOpenInvoc(*fresh)
	}
}

func (a *Allocator) onEnd(e End) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.cancelIDETimer()

	if a.open == nil {
		a.mintEndOnlyBucket(e)
		a.openSpec(a.now())

		return
	}

	if isSpec(a.open) {
		a.promoteSpecToEnd(e)
		a.openSpec(a.now())

		return
	}

	bucket := a.open
	a.open = nil

	bucket.EndAt = e.At
	bucket.Scheme = e.Scheme
	bucket.Project = e.Project
	bucket.Workspace = e.Workspace
	bucket.Configuration = e.Configuration
	if e.Action != "" {
		bucket.Action = e.Action
	}
	bucket.Success = e.Success
	bucket.SuccessKnown = true
	bucket.Duration = e.Duration
	bucket.Reason = BucketReasonEndMatched
	bucket.BlobStats = a.snapshotBlobs()
	bucket.XCActivityLogPath = e.XCActivityLogPath

	a.infof("Allocator: bucket closed id=%s bytes=%d", bucket.InvocationID, snapshotBytes(bucket.BlobStats))

	a.emit(bucket)
	if bucket.Scheme != "" {
		a.schemeEmittedSinceStart = true
	}
	a.openSpec(a.now())
}

// promoteSpecToEnd turns the currently-open speculative bucket into the warm
// rebuild's final invocation row. Keeps the spec's InvocationID — any CAS
// RPCs the proxy stamped during the warm build (while no real Start fired)
// already carry that ID, so BE DataTransfer rows join to the promoted row.
//
// Non-primary signatures (Signing / Packages / Localization) leave the spec
// in place — they mark sub-builds, not a user-visible build.
func (a *Allocator) promoteSpecToEnd(e End) {
	if e.Action == "" || e.UniqueIdentifier == "" {
		a.debugf("Allocator: spec-promote drop scheme=%s action=%q (not primary)", e.Scheme, e.Action)

		return
	}

	bucket := a.open
	a.open = nil

	bucket.StartAt = e.At.Add(-e.Duration)
	bucket.EndAt = e.At
	bucket.Scheme = e.Scheme
	bucket.Project = e.Project
	bucket.Workspace = e.Workspace
	bucket.Configuration = e.Configuration
	bucket.Action = e.Action
	bucket.Success = e.Success
	bucket.SuccessKnown = true
	bucket.Duration = e.Duration
	bucket.Reason = BucketReasonEndMatched
	bucket.BlobStats = a.snapshotBlobs()
	bucket.XCActivityLogPath = e.XCActivityLogPath

	a.infof("Allocator: spec-promote End id=%s scheme=%s bytes=%d", bucket.InvocationID, bucket.Scheme, snapshotBytes(bucket.BlobStats))

	a.emit(bucket)
	if bucket.Scheme != "" {
		a.schemeEmittedSinceStart = true
	}
}

// mintEndOnlyBucket is the sole-End authoritative path: when Xcode reuses an
// existing xcbuilddata dir on a warm ⌘B (no CREATE event → no Start event),
// the LogStoreManifest seal is still a complete signal. Mint a one-shot bucket
// keyed on the manifest UUID and emit slim+enriched back-to-back. Called only
// when a.open == nil so the Start-paired flow is unaffected.
//
// Non-primary scheme entries (Signing / Packages / Localization sub-builds)
// resolve to End.Action == "" via actionFromSignature and short-circuit here —
// they are Xcode scaffolding, not user-visible builds.
func (a *Allocator) mintEndOnlyBucket(e End) {
	if e.Action == "" || e.UniqueIdentifier == "" {
		a.debugf("Allocator: sole-End drop scheme=%s action=%q (not primary)", e.Scheme, e.Action)

		return
	}

	anchor := buildidentity.Anchor{
		Hostname:        a.Hostname,
		PID:             a.ProxyPID,
		StartTimeMS:     a.ProxyStartMS,
		BuildRequestHex: e.UniqueIdentifier,
	}

	bucket := &Bucket{
		InvocationID:      buildidentity.Derive(anchor),
		StartAt:           e.At.Add(-e.Duration),
		EndAt:             e.At,
		Scheme:            e.Scheme,
		Project:           e.Project,
		Workspace:         e.Workspace,
		Configuration:     e.Configuration,
		Action:            e.Action,
		Success:           e.Success,
		SuccessKnown:      true,
		Duration:          e.Duration,
		Reason:            BucketReasonEndMatched,
		PeerPID:           a.ProxyPID,
		PeerStartMS:       a.ProxyStartMS,
		BlobStats:         a.snapshotBlobs(),
		XCActivityLogPath: e.XCActivityLogPath,
	}

	a.infof("Allocator: sole-End mint id=%s scheme=%s bytes=%d", bucket.InvocationID, bucket.Scheme, snapshotBytes(bucket.BlobStats))

	if a.OnOpenInvoc != nil {
		a.OnOpenInvoc(*bucket)
	}

	a.emit(bucket)
	if bucket.Scheme != "" {
		a.schemeEmittedSinceStart = true
	}
}

func (a *Allocator) onQuit(q Quit) {
	switch {
	case q.AncestryIDE:
		a.scheduleIDEFlush(q)
	default:
		a.flushCLI(q)
	}
}

func (a *Allocator) scheduleIDEFlush(q Quit) {
	timeout := a.IDEBackgroundTO
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	a.ideTimerMu.Lock()
	if a.ideTimer != nil {
		a.ideTimer.Stop()
	}
	a.ideTimer = time.AfterFunc(timeout, func() {
		a.flushIDEBackground(q)
	})
	a.ideTimerMu.Unlock()
}

func (a *Allocator) cancelIDETimer() {
	a.ideTimerMu.Lock()
	defer a.ideTimerMu.Unlock()

	if a.ideTimer != nil {
		a.ideTimer.Stop()
		a.ideTimer = nil
	}
}

func (a *Allocator) flushIDEBackground(q Quit) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.ideTimerMu.Lock()
	a.ideTimer = nil
	a.ideTimerMu.Unlock()

	// AfterFunc fires outside the lock; a non-spec Start can land between the
	// timer trigger and the lock acquisition, opening a new bucket that
	// belongs to the next build, not to this Quit. Specs do NOT count — they
	// carry the trailing IDE traffic the Quit meant to classify.
	if a.open != nil && !isSpec(a.open) && a.open.StartAt.After(q.At) {
		a.debugf("Allocator: IDE flush skipped, newer Start at %s > quit at %s", a.open.StartAt, q.At)

		return
	}

	if a.open != nil {
		bucket := a.open
		a.open = nil

		bucket.EndAt = a.now()
		if bucket.Reason == BucketReasonSpeculative {
			bucket.Reason = BucketReasonIDEBackground
		} else {
			bucket.Reason = BucketReasonEndOfPrior
		}
		bucket.BlobStats = a.snapshotBlobs()
		if hasActivity(bucket.BlobStats) || bucket.Reason != BucketReasonIDEBackground {
			a.emit(bucket)
		}

		a.openSpec(a.now())

		return
	}

	snap := a.snapshotBlobs()
	if hasActivity(snap) {
		bg := a.ideBackgroundBucket(q, snap)
		a.emit(&bg)
	}

	a.openSpec(a.now())
}

func (a *Allocator) flushCLI(q Quit) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.cancelIDETimer()

	if a.schemeEmittedSinceStart {
		a.debugf("Allocator: flushCLI suppressed — scheme bucket already resolved this cycle")
		a.open = nil
		a.openSpec(a.now())

		return
	}

	if a.open != nil {
		bucket := a.open
		a.open = nil

		bucket.EndAt = q.At
		bucket.Reason = BucketReasonCLIImplicit
		bucket.BlobStats = a.snapshotBlobs()
		if hasActivity(bucket.BlobStats) || !isSpec(bucket) {
			a.emit(bucket)
		}

		a.openSpec(a.now())

		return
	}

	snap := a.snapshotBlobs()
	if hasActivity(snap) {
		bucket := a.cliImplicitBucket(q, snap)
		a.emit(&bucket)
	}

	a.openSpec(a.now())
}

// newBucket mints the InvocationID + metadata for a Start. ProxyPID /
// ProxyStartMS are used as the peer anchor on the derived side when the
// pipeline has not yet learned the peer PID (the Pipeline overwrites PeerPID
// / PeerStartMS once a CAS conn arrives — the ID itself is derived off the
// build-request hex, not the peer PID, so it stays stable).
func (a *Allocator) newBucket(at time.Time, s Start) *Bucket {
	anchor := buildidentity.Anchor{
		Hostname:        a.Hostname,
		PID:             a.ProxyPID,
		StartTimeMS:     a.ProxyStartMS,
		BuildRequestHex: s.BuildRequestHex,
	}

	return &Bucket{
		InvocationID:    buildidentity.Derive(anchor),
		StartAt:         at,
		Action:          s.Action,
		BuildRequestHex: s.BuildRequestHex,
		PeerPID:         a.ProxyPID,
		PeerStartMS:     a.ProxyStartMS,
	}
}

func (a *Allocator) ideBackgroundBucket(q Quit, snap *blobstats.Snapshot) Bucket {
	peerPID := q.PID
	if peerPID == 0 {
		peerPID = a.ProxyPID
	}

	// Mirror cliImplicitBucket: prefer the peer's start-ms (bound at
	// TagConn) so two IDE Quits with the same PID but different start-ms
	// derive distinct InvocationIDs. A PID recycle otherwise collapses the
	// two sessions onto one BE row.
	peerStartMS := q.StartTimeMS
	if peerStartMS == 0 {
		peerStartMS = a.ProxyStartMS
	}

	anchor := buildidentity.Anchor{
		Hostname:        a.Hostname,
		PID:             peerPID,
		StartTimeMS:     peerStartMS,
		BuildRequestHex: AnchorSaltIDEBackground,
	}

	start := q.At
	end := a.now()

	return Bucket{
		InvocationID: buildidentity.Derive(anchor),
		StartAt:      start,
		EndAt:        end,
		Action:       PrimaryAction("idle"),
		Reason:       BucketReasonIDEBackground,
		PeerPID:      peerPID,
		PeerStartMS:  peerStartMS,
		BlobStats:    snap,
	}
}

func (a *Allocator) cliImplicitBucket(q Quit, snap *blobstats.Snapshot) Bucket {
	peerPID := q.PID
	if peerPID == 0 {
		peerPID = a.ProxyPID
	}

	peerStartMS := q.StartTimeMS
	if peerStartMS == 0 {
		peerStartMS = a.ProxyStartMS
	}

	anchor := buildidentity.Anchor{
		Hostname:        a.Hostname,
		PID:             peerPID,
		StartTimeMS:     peerStartMS,
		BuildRequestHex: AnchorSaltCLIRun,
	}

	return Bucket{
		InvocationID: buildidentity.Derive(anchor),
		StartAt:      q.At,
		EndAt:        q.At,
		Reason:       BucketReasonCLIImplicit,
		PeerPID:      peerPID,
		PeerStartMS:  peerStartMS,
		BlobStats:    snap,
	}
}

func (a *Allocator) emit(b *Bucket) {
	if a.OnReady == nil || b == nil {
		return
	}

	a.OnReady(*b)
}

func (a *Allocator) snapshotBlobs() *blobstats.Snapshot {
	if a.BlobStats == nil {
		return nil
	}

	return a.BlobStats.TakeSnapshot()
}

func hasActivity(snap *blobstats.Snapshot) bool {
	if snap == nil {
		return false
	}

	return !snap.IsEmpty()
}

func snapshotBytes(snap *blobstats.Snapshot) int64 {
	if snap == nil {
		return 0
	}

	return snap.Upload.BytesTotal + snap.Download.BytesTotal
}

func (a *Allocator) infof(format string, args ...any) {
	if a.Logger == nil {
		return
	}

	a.Logger.Infof(format, args...)
}

func (a *Allocator) debugf(format string, args ...any) {
	if a.Logger == nil {
		return
	}

	a.Logger.Debugf(format, args...)
}

func (a *Allocator) warnf(format string, args ...any) {
	if a.Logger == nil {
		return
	}

	a.Logger.Warnf(format, args...)
}
