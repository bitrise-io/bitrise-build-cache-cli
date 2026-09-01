package kv

import (
	"context"
	"fmt"
	"runtime"
	"runtime/metrics"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Build VMs run no monitoring agent, so when cache operations time out there is
// no way to tell afterwards whether the process was starved of CPU or simply
// out of channel slots. These snapshots put that in the log the operation
// already writes.

const (
	poolSampleInterval = 30 * time.Second
	// A saturated pool fails many operations at once; one line per burst is
	// enough to characterise it.
	contentionLogInterval = 5 * time.Second
	schedLatencyMetric    = "/sched/latencies:seconds"
	memTotalMetric        = "/memory/classes/total:bytes"
)

type poolSnapshot struct {
	channels int
	perLimit int
	inFlight int
	capacity int
}

func (p poolSnapshot) String() string {
	return fmt.Sprintf("pool %d/%d slots busy (%d channels x %d)",
		p.inFlight, p.capacity, p.channels, p.perLimit)
}

// connStates counts each channel's gRPC connectivity state. A full pool reads
// the same whether it is genuinely busy or holding slots on a wedged transport.
func (c *Client) connStates() string {
	counts := map[string]int{}
	for _, ch := range c.channels {
		if ch.conn == nil {
			continue
		}
		counts[ch.conn.GetState().String()]++
	}
	if len(counts) == 0 {
		return "conns=n/a"
	}

	states := make([]string, 0, len(counts))
	for state, n := range counts {
		states = append(states, fmt.Sprintf("%s:%d", state, n))
	}
	sort.Strings(states)

	return "conns " + strings.Join(states, ",")
}

// logContention reports why operations are failing, rate-limited so a burst
// costs one line rather than thousands.
func (c *Client) logContention() {
	if c.logger == nil {
		return
	}

	now := time.Now().UnixNano()
	last := c.lastContentionLog.Load()
	if now-last < int64(contentionLogInterval) {
		return
	}
	if !c.lastContentionLog.CompareAndSwap(last, now) {
		return
	}

	c.logger.Warnf("Cache operation failed: %s", c.contentionLine())
}

// acquireOn takes a slot and, on failure, records what the pool and the
// process looked like at that moment.
func (c *Client) acquireOn(ctx context.Context, ch *channel) error {
	if err := ch.acquire(ctx); err != nil {
		c.logContention()

		return err
	}

	return nil
}

func (c *Client) poolSnapshot() poolSnapshot {
	snap := poolSnapshot{channels: len(c.channels)}
	for _, ch := range c.channels {
		if ch.sem == nil {
			continue
		}
		snap.inFlight += len(ch.sem)
		snap.capacity += cap(ch.sem)
		snap.perLimit = cap(ch.sem)
	}

	return snap
}

type contentionSnapshot struct {
	goroutines int
	// Time a goroutine sat runnable before it got a thread: rises when the OS
	// is not scheduling this process, which is what throttling looks like from
	// the inside.
	schedLatencyP99        time.Duration
	involuntaryCtxSwitches int64
	majorPageFaults        int64
	blocksIn               int64
	blocksOut              int64
	// cpuTime is the kernel's own counter, read as-is: a rate needs two of
	// these, and sched_p99 already answers whether the process is starved.
	cpuTime     time.Duration
	memBytes    int64
	maxRSSBytes int64
}

func (s contentionSnapshot) String() string {
	return fmt.Sprintf(
		"cpu_time=%s mem=%dMB max_rss=%dMB goroutines=%d sched_p99=%s involuntary_switches=%d major_faults=%d blocks_in=%d blocks_out=%d",
		s.cpuTime.Round(time.Millisecond), s.memBytes/(1024*1024), s.maxRSSBytes/(1024*1024),
		s.goroutines, s.schedLatencyP99, s.involuntaryCtxSwitches,
		s.majorPageFaults, s.blocksIn, s.blocksOut)
}

// The int64 conversions below look redundant on 64-bit but are not: Rusage
// counters are int32 on linux/386, which goreleaser still builds.
//
//nolint:unconvert // required for 32-bit targets
func sampleContention() contentionSnapshot {
	snap := contentionSnapshot{goroutines: runtime.NumGoroutine()}

	samples := []metrics.Sample{{Name: schedLatencyMetric}, {Name: memTotalMetric}}
	metrics.Read(samples)
	if h := samples[0].Value; h.Kind() == metrics.KindFloat64Histogram {
		snap.schedLatencyP99 = histogramQuantile(h.Float64Histogram(), 0.99)
	}
	if m := samples[1].Value; m.Kind() == metrics.KindUint64 {
		snap.memBytes = int64(m.Uint64()) //nolint:gosec // process memory, far below int64
	}

	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err == nil {
		snap.involuntaryCtxSwitches = int64(ru.Nivcsw)
		snap.majorPageFaults = int64(ru.Majflt)
		snap.blocksIn = int64(ru.Inblock)
		snap.blocksOut = int64(ru.Oublock)
		snap.maxRSSBytes = maxRSSBytes(int64(ru.Maxrss))
		snap.cpuTime = timevalDuration(ru.Utime) + timevalDuration(ru.Stime)
	}

	return snap
}

// Darwin reports ru_maxrss in bytes, Linux and the BSDs in kilobytes.
func maxRSSBytes(maxrss int64) int64 {
	if runtime.GOOS == "darwin" {
		return maxrss
	}

	return maxrss * 1024
}

func timevalDuration(tv syscall.Timeval) time.Duration {
	return time.Duration(tv.Sec)*time.Second + time.Duration(tv.Usec)*time.Microsecond
}

// contentionLine is the shared body of both the periodic sample and the
// on-error report.
func (c *Client) contentionLine() string {
	return fmt.Sprintf("%s | %s | %s",
		c.poolSnapshot(), c.connStates(), sampleContention())
}

func histogramQuantile(h *metrics.Float64Histogram, q float64) time.Duration {
	if h == nil {
		return 0
	}

	var total uint64
	for _, c := range h.Counts {
		total += c
	}
	if total == 0 {
		return 0
	}

	target := uint64(float64(total) * q)

	var seen uint64
	for i, c := range h.Counts {
		seen += c
		if seen >= target {
			// Buckets is one longer than Counts: bucket i spans [i, i+1).
			return time.Duration(h.Buckets[i+1] * float64(time.Second))
		}
	}

	return time.Duration(h.Buckets[len(h.Buckets)-1] * float64(time.Second))
}

func (c *Client) samplePool(stop <-chan struct{}) {
	ticker := time.NewTicker(poolSampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			c.logger.Debugf("Cache %s", c.contentionLine())
		}
	}
}
