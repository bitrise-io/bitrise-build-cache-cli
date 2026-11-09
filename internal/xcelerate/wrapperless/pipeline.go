package wrapperless

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildrequest"
)

type ProxyControl interface {
	SetCurrentBucketID(id string)
}

type Pipeline struct {
	HomeDir      string
	Logger       log.Logger
	Emitter      *Emitter
	Allocator    *Allocator
	Timeline     *Timeline
	StartWatcher *StartWatcher
	EndWatcher   *EndWatcher
	QuitWatcher  *QuitWatcher
	Proxy        ProxyControl
	// BlobStats, when non-nil, is attached to the Allocator at Start so each
	// emitted Bucket carries the windowed blob-transfer stats for the period
	// since the previous bucket.
	BlobStats BlobStatsSource

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	done    chan struct{}
}

func (p *Pipeline) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()

		return nil
	}
	p.started = true

	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	done := make(chan struct{})
	p.done = done
	p.mu.Unlock()

	if p.Timeline == nil {
		p.Timeline = NewTimeline()
	}
	if p.Allocator == nil {
		hostname, pid, startMs := hostAndSelf()
		p.Allocator = NewAllocator(hostname, pid, startMs)
	}

	p.Allocator.OnReady = p.onBucketReady
	p.Allocator.OnOpenInvoc = p.onBucketOpen
	if p.Allocator.Logger == nil {
		p.Allocator.Logger = p.Logger
	}
	if p.BlobStats != nil && p.Allocator.BlobStats == nil {
		p.Allocator.BlobStats = p.BlobStats
	}

	if p.StartWatcher == nil {
		p.StartWatcher = &StartWatcher{HomeDir: p.HomeDir, Logger: p.Logger}
	}
	p.StartWatcher.OnStart = p.Timeline.PushStart

	if p.EndWatcher == nil {
		p.EndWatcher = &EndWatcher{HomeDir: p.HomeDir, Logger: p.Logger}
	}
	p.EndWatcher.OnEnd = p.Timeline.PushEnd

	if p.QuitWatcher == nil {
		p.QuitWatcher = &QuitWatcher{Logger: p.Logger}
	}
	p.QuitWatcher.OnQuit = p.Timeline.PushQuit

	if err := p.StartWatcher.Start(ctx); err != nil {
		cancel()

		return err
	}
	if err := p.EndWatcher.Start(ctx); err != nil {
		cancel()

		return err
	}
	p.QuitWatcher.Start(ctx)

	go func() {
		defer close(done)
		p.Allocator.Run(ctx, p.Timeline)
	}()

	p.recoverFromDerivedData()

	return nil
}

func (p *Pipeline) Close() {
	p.mu.Lock()
	cancel := p.cancel
	done := p.done
	p.cancel = nil
	p.done = nil
	p.started = false
	p.mu.Unlock()

	if p.StartWatcher != nil {
		p.StartWatcher.Close()
	}
	if p.EndWatcher != nil {
		p.EndWatcher.Close()
	}
	if p.QuitWatcher != nil {
		p.QuitWatcher.Close()
	}

	if p.Timeline != nil {
		p.Timeline.Close()
	}

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// RegisterPeer feeds a peer PID into the QuitWatcher. Callers (the proxy)
// invoke this on each TagConn so the pipeline learns whom to watch.
func (p *Pipeline) RegisterPeer(pid int, ancestry Ancestry, startTimeMS int64) {
	if p.QuitWatcher == nil {
		return
	}

	p.QuitWatcher.Register(pid, ancestry, startTimeMS)
}

func (p *Pipeline) onBucketOpen(invocationID string) {
	if invocationID == "" {
		return
	}

	if p.Proxy != nil {
		p.Proxy.SetCurrentBucketID(invocationID)
	}

	if p.Emitter != nil {
		p.Emitter.EmitSlim(Bucket{InvocationID: invocationID, StartAt: time.Now()})
	}
}

func (p *Pipeline) onBucketReady(b Bucket) {
	if p.Emitter != nil {
		p.Emitter.EmitEnriched(b, 0)
	}
}

func hostAndSelf() (string, int, int64) {
	host, _ := os.Hostname()
	pid := os.Getpid()
	startMs := time.Now().UnixMilli()

	return host, pid, startMs
}

// recoverFromDerivedData walks every DD root's XCBuildData dirs on Start,
// synthesizing a Start event per primary xcbuilddata and a matching End
// event when the sibling LogStoreManifest.plist carries a sealed entry.
// Unsealed primaries inject only Start so a later fsnotify End can close the
// bucket; the EndWatcher's seen set is pre-marked so the live fsnotify tick
// does not double-fire on the already-synthesized End.
func (p *Pipeline) recoverFromDerivedData() {
	for _, root := range p.derivedDataRoots() {
		for _, xcbuildDataDir := range xcbuildDataDirsFor(root) {
			p.recoverXcbuildData(root, xcbuildDataDir)
		}
		p.markRecoveredManifest(root)
	}
}

func (p *Pipeline) recoverXcbuildData(root, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, xcbuilddataSuffix) {
			continue
		}

		hex := strings.TrimSuffix(name, xcbuilddataSuffix)
		info, err := buildrequest.Load(root, hex)
		if err != nil {
			continue
		}

		action, ok := classifyPrimary(info)
		if !ok {
			continue
		}

		start := Start{
			At:              time.Now(),
			DerivedDataPath: root,
			BuildRequestHex: hex,
			Action:          action,
		}
		p.Timeline.PushStart(start)
	}
}

// markRecoveredManifest walks the manifest at DD/Logs/Build/... and, for
// each sealed entry, injects a synthetic End + pre-marks the EndWatcher so
// the live tick doesn't re-emit the same entry.
func (p *Pipeline) markRecoveredManifest(root string) {
	manifestPath := filepath.Join(root, "Logs", buildDirName, "LogStoreManifest.plist")
	entries, err := loadManifestEntries(manifestPath)
	if err != nil {
		return
	}

	for _, e := range entries {
		if !isSealedTerminal(e.HighLevelStatus) {
			continue
		}
		if e.UniqueIdentifier == "" || e.TimeStopped == 0 {
			continue
		}

		if p.EndWatcher != nil {
			p.EndWatcher.MarkSeen(e.UniqueIdentifier)
		}

		p.Timeline.PushEnd(End{
			At:               time.Now(),
			Scheme:           schemeFromSignature(e.Signature),
			Success:          e.HighLevelStatus == "S",
			UniqueIdentifier: e.UniqueIdentifier,
			Duration:         cfAbsDuration(e.TimeStarted, e.TimeStopped),
		})
	}
}

func (p *Pipeline) derivedDataRoots() []string {
	home := p.HomeDir
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		home = h
	}

	matches, err := filepath.Glob(filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData", "*"))
	if err != nil {
		return nil
	}

	return matches
}
