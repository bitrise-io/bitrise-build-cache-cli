package wrapperless

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
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
func (p *Pipeline) RegisterPeer(pid int, ancestry Ancestry) {
	if p.QuitWatcher == nil {
		return
	}

	p.QuitWatcher.Register(pid, ancestry)
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
