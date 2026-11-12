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
	HomeDir     string
	Logger      log.Logger
	Emitter     *Emitter
	Allocator   *Allocator
	DDWatcher   *DDWatcher
	QuitWatcher *QuitWatcher
	Proxy       ProxyControl
	// BlobStats, when non-nil, is attached to the Allocator at Start so each
	// emitted Bucket carries the windowed blob-transfer stats for the period
	// since the previous bucket.
	BlobStats BlobStatsSource

	// PutQueueDepth sets the capacity of the async PUT worker queue. Keeps
	// HTTP calls off the Allocator's single timeline reader (which holds
	// a.mu across every event). 0 → defaultPutQueueDepth.
	PutQueueDepth int
	// PutDrainTimeout caps the time Close waits for the worker to flush
	// remaining jobs. Anything still queued at the deadline is persisted to
	// the Emitter's pending-ndjson for the next proxy start to replay.
	// 0 → defaultPutDrainTimeout.
	PutDrainTimeout time.Duration

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	done    chan struct{}

	putQueue chan putJob
	putDone  chan struct{}
}

type putJob struct {
	phase   Phase
	bucket  Bucket
	hitRate float32
}

const (
	defaultPutQueueDepth   = 1024
	defaultPutDrainTimeout = 30 * time.Second
)

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

	if p.DDWatcher == nil {
		p.DDWatcher = &DDWatcher{HomeDir: p.HomeDir, Logger: p.Logger} //nolint:exhaustruct
	}
	p.DDWatcher.OnStart = p.Allocator.PushStart
	p.DDWatcher.OnEnd = p.Allocator.PushEnd

	if p.QuitWatcher == nil {
		p.QuitWatcher = &QuitWatcher{Logger: p.Logger} //nolint:exhaustruct
	}
	p.QuitWatcher.OnQuit = p.Allocator.PushQuit

	if err := p.DDWatcher.Start(ctx); err != nil {
		cancel()

		return err
	}
	p.QuitWatcher.Start(ctx)

	if p.Emitter != nil {
		p.Emitter.Start(ctx)
	}

	depth := p.PutQueueDepth
	if depth <= 0 {
		depth = defaultPutQueueDepth
	}
	putQueue := make(chan putJob, depth)
	putDone := make(chan struct{})

	p.mu.Lock()
	p.putQueue = putQueue
	p.putDone = putDone
	p.mu.Unlock()

	go p.putWorker(putQueue, putDone)

	p.Allocator.Init()

	go func() {
		defer close(done)
		p.Allocator.Run(ctx)
	}()

	return nil
}

func (p *Pipeline) Close() {
	p.mu.Lock()
	cancel := p.cancel
	done := p.done
	putQueue := p.putQueue
	putDone := p.putDone
	p.cancel = nil
	p.done = nil
	p.putQueue = nil
	p.putDone = nil
	p.started = false
	p.mu.Unlock()

	if p.DDWatcher != nil {
		p.DDWatcher.Close()
	}
	if p.QuitWatcher != nil {
		p.QuitWatcher.Close()
	}

	if p.Allocator != nil {
		p.Allocator.CloseIntake()
	}

	if done != nil {
		<-done
	}

	if putQueue != nil {
		close(putQueue)

		timeout := p.PutDrainTimeout
		if timeout <= 0 {
			timeout = defaultPutDrainTimeout
		}

		select {
		case <-putDone:
		case <-time.After(timeout):
			p.warnf("Pipeline: put worker exceeded %s drain budget; remaining jobs persisted", timeout)
			p.flushQueueToPending(putQueue)
			<-putDone
		}
	}

	if cancel != nil {
		cancel()
	}

	if p.Emitter != nil {
		p.Emitter.Stop()
	}
}

func (p *Pipeline) putWorker(queue chan putJob, done chan struct{}) {
	defer close(done)

	for job := range queue {
		switch job.phase {
		case PhaseSlim:
			if p.Emitter != nil {
				p.Emitter.EmitSlim(job.bucket)
			}
		case PhaseEnriched:
			if p.Emitter != nil {
				p.Emitter.EmitEnriched(job.bucket, job.hitRate)
			}
		}
	}
}

func (p *Pipeline) enqueue(job putJob) {
	p.mu.Lock()
	q := p.putQueue
	p.mu.Unlock()

	if q == nil {
		// Pre-Start or post-Close: fall back to the sync path so no row is lost.
		p.directEmit(job)

		return
	}

	select {
	case q <- job:
	default:
		p.warnf("Pipeline: put queue full, persisting phase=%s id=%s", job.phase, job.bucket.InvocationID)
		p.persistJob(job)
	}
}

func (p *Pipeline) directEmit(job putJob) {
	if p.Emitter == nil {
		return
	}
	switch job.phase {
	case PhaseSlim:
		p.Emitter.EmitSlim(job.bucket)
	case PhaseEnriched:
		p.Emitter.EmitEnriched(job.bucket, job.hitRate)
	}
}

func (p *Pipeline) persistJob(job putJob) {
	if p.Emitter == nil {
		return
	}

	p.Emitter.persistPending(Payload{
		Phase:   job.phase,
		Bucket:  job.bucket,
		HitRate: job.hitRate,
	})
}

func (p *Pipeline) flushQueueToPending(q chan putJob) {
	for {
		select {
		case job, ok := <-q:
			if !ok {
				return
			}
			p.persistJob(job)
		default:
			return
		}
	}
}

func (p *Pipeline) warnf(format string, args ...any) {
	if p.Logger == nil {
		return
	}

	p.Logger.Warnf(format, args...)
}

// RegisterPeer feeds a peer PID into the QuitWatcher. Callers (the proxy)
// invoke this on each TagConn so the pipeline learns whom to watch.
func (p *Pipeline) RegisterPeer(pid int, ancestry Ancestry, startTimeMS int64) {
	if p.QuitWatcher == nil {
		return
	}

	p.QuitWatcher.Register(pid, ancestry, startTimeMS)
}

func (p *Pipeline) onBucketOpen(bucket Bucket) {
	if bucket.InvocationID == "" {
		return
	}

	if p.Proxy != nil {
		p.Proxy.SetCurrentBucketID(bucket.InvocationID)
	}

	// Speculative buckets exist to keep CurrentBucketID live between builds;
	// they get classified (and slim-emitted) at the next Start / End / Quit.
	if bucket.Reason == BucketReasonSpeculative {
		return
	}

	slim := Bucket{ //nolint:exhaustruct // slim-phase payload
		InvocationID:  bucket.InvocationID,
		StartAt:       time.Now(),
		Scheme:        bucket.Scheme,
		Project:       bucket.Project,
		Workspace:     bucket.Workspace,
		Configuration: bucket.Configuration,
		Action:        bucket.Action,
		Reason:        bucket.Reason,
	}
	p.enqueue(putJob{phase: PhaseSlim, bucket: slim})
}

func (p *Pipeline) onBucketReady(b Bucket) {
	p.enqueue(putJob{phase: PhaseEnriched, bucket: b, hitRate: computeHitRate(b)})
}

func hostAndSelf() (string, int, int64) {
	host, _ := os.Hostname()
	pid := os.Getpid()
	startMs := time.Now().UnixMilli()

	return host, pid, startMs
}
