package wrapperless

import (
	"context"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildidentity"
)

// BuildRequestHexIDEBackground anchors the IDE-background bucket's derived
// InvocationID so trailing CAS traffic after a Quit{IDE} lands on its own
// BE row.
const BuildRequestHexIDEBackground = "ide-background"

// BuildRequestHexCLIRun anchors the implicit-END bucket's derived
// InvocationID so a Quit{CLI} with no End still flushes to a stable row.
const BuildRequestHexCLIRun = "cli-run"

// BlobStatsSource is the Allocator's windowed-stats seam. TakeSnapshot must
// snapshot and reset atomically so an RPC interleaving between the two does
// not land in both the current bucket and the next one.
type BlobStatsSource interface {
	TakeSnapshot() *blobstats.Snapshot
}

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
}

func NewAllocator(hostname string, proxyPID int, proxyStartMS int64) *Allocator {
	return &Allocator{
		Hostname:     hostname,
		ProxyPID:     proxyPID,
		ProxyStartMS: proxyStartMS,
	}
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

func (a *Allocator) Run(ctx context.Context, timeline *Timeline) {
	a.mu.Lock()
	if a.done == nil {
		a.done = make(chan struct{})
	}
	done := a.done
	a.mu.Unlock()

	defer close(done)

	timeline.Drain(ctx, a.Handle)
}

// Done blocks until Run returns. nil-safe for tests that drive Handle
// directly without ever calling Run.
func (a *Allocator) Done() {
	a.mu.Lock()
	done := a.done
	a.mu.Unlock()

	if done != nil {
		<-done
	}
}

func (a *Allocator) onStart(s Start) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.cancelIDETimer()

	// Single-slot design: a second concurrent Start evicts the prior as
	// EndOfPrior rather than opening a parallel bucket. Correct for serial
	// IDE ⌘B and sequential CLI runs; parallel CLI builds from the same
	// shell get interleaved attribution — proxy's global CurrentBucketID
	// stamps every RPC with the latest, so the prior bucket closes with
	// whatever bytes it saw before the swap. Per-peer-PID attribution is
	// tracked as a follow-up.
	if a.open != nil {
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

	a.debugf("Allocator: bucket opened id=%s action=%s", fresh.InvocationID, fresh.Action)

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

	a.debugf("Allocator: bucket closed id=%s bytes=%d", bucket.InvocationID, snapshotBytes(bucket.BlobStats))

	a.emit(bucket)
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
		InvocationID:  buildidentity.Derive(anchor),
		StartAt:       e.At.Add(-e.Duration),
		EndAt:         e.At,
		Scheme:        e.Scheme,
		Project:       e.Project,
		Workspace:     e.Workspace,
		Configuration: e.Configuration,
		Action:        e.Action,
		Success:       e.Success,
		SuccessKnown:  true,
		Duration:      e.Duration,
		Reason:        BucketReasonEndMatched,
		PeerPID:       a.ProxyPID,
		PeerStartMS:   a.ProxyStartMS,
		BlobStats:     a.snapshotBlobs(),
	}

	a.debugf("Allocator: sole-End mint id=%s scheme=%s bytes=%d", bucket.InvocationID, bucket.Scheme, snapshotBytes(bucket.BlobStats))

	if a.OnOpenInvoc != nil {
		a.OnOpenInvoc(*bucket)
	}

	a.emit(bucket)
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

	if a.open != nil {
		bucket := a.open
		a.open = nil

		bucket.EndAt = a.now()
		bucket.Reason = BucketReasonEndOfPrior
		bucket.BlobStats = a.snapshotBlobs()
		a.emit(bucket)
	}

	snap := a.snapshotBlobs()
	if !hasActivity(snap) {
		return
	}

	bg := a.ideBackgroundBucket(q, snap)
	a.emit(&bg)
}

func (a *Allocator) flushCLI(q Quit) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.cancelIDETimer()

	if a.open != nil {
		bucket := a.open
		a.open = nil

		bucket.EndAt = q.At
		bucket.Reason = BucketReasonCLIImplicit
		bucket.BlobStats = a.snapshotBlobs()
		a.emit(bucket)

		return
	}

	snap := a.snapshotBlobs()
	if !hasActivity(snap) {
		return
	}

	bucket := a.cliImplicitBucket(q, snap)
	a.emit(&bucket)
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

	anchor := buildidentity.Anchor{
		Hostname:        a.Hostname,
		PID:             peerPID,
		StartTimeMS:     a.ProxyStartMS,
		BuildRequestHex: BuildRequestHexIDEBackground,
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
		PeerStartMS:  a.ProxyStartMS,
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
		BuildRequestHex: BuildRequestHexCLIRun,
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
