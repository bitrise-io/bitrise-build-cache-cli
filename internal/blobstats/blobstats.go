// Package blobstats records per-blob cache transfer distributions per direction, in the payload
// shape of the Gradle plugins' io.bitrise.gradle.common.CacheBlobStats.
package blobstats

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
)

// Bumped when a field's meaning changes, not when a boundary moves — boundaries are sent too.
const SchemaVersion = 1

// Size and throughput must move together with BlobStatsBuckets in gradle-plugins; latency
// deliberately differs, because macOS overflows Gradle's 2..512 scale ~20x more.
//
//nolint:gochecknoglobals // fixed scale, shared by every recorder
var (
	// ×2 from 4 ms: a ≤2 ms bucket held 0.06% of downloads, so the step went to the tail.
	LatencyMsBuckets = []int64{4, 8, 16, 32, 64, 128, 256, 512, 1_024}

	// ×4 from 512 B: downloads sit in the bottom four buckets, uploads spread across seven.
	SizeBytesBuckets = []int64{512, 2_048, 8_192, 32_768, 131_072, 524_288, 2_097_152, 8_388_608, 33_554_432}

	// ×2 from 256 kB/s: nothing observed above 128 MB/s, and 1.1% of downloads at or below 512 kB/s.
	ThroughputBytesPerSecBuckets = []int64{
		262_144, 524_288, 1_048_576, 2_097_152, 4_194_304,
		8_388_608, 16_777_216, 33_554_432, 67_108_864,
	}
)

const (
	// Below this, per-op throughput measures the round trip, not the bandwidth: including small
	// blobs drops the download median from 7.32 to 0.16 MB/s. They still count in latency and size.
	ThroughputMinBlobBytes = 16_384

	// Past this, percentiles come from the samples retained so far.
	maxThroughputSamples = 200_000
)

type HistogramSnapshot struct {
	Boundaries []int64 `json:"boundaries"`
	// One longer than Boundaries; the last entry is the overflow bucket.
	Counts []int64 `json:"counts"`
	Count  int64   `json:"count"`
	Sum    int64   `json:"sum"`
	Min    int64   `json:"min"`
	Max    int64   `json:"max"`
}

// The percentiles are exact; the histogram is what makes them mergeable across invocations.
type ThroughputSnapshot struct {
	Histogram        HistogramSnapshot `json:"histogram"`
	P10BytesPerSec   int64             `json:"p10BytesPerSec"`
	P50BytesPerSec   int64             `json:"p50BytesPerSec"`
	P90BytesPerSec   int64             `json:"p90BytesPerSec"`
	MinBlobBytes     int64             `json:"minBlobBytes"`
	ExcludedSmallOps int64             `json:"excludedSmallOps"`
}

type DirectionSnapshot struct {
	OpCount    int64 `json:"opCount"`
	ErrorCount int64 `json:"errorCount"`
	// Downloads only, counted rather than timed: a cheap round trip would pull the latency down.
	MissCount int64 `json:"missCount"`
	// Uploads only: already stored this session, so nothing went over the wire.
	SkippedAlreadySavedCount int64              `json:"skippedAlreadySavedCount"`
	BytesTotal               int64              `json:"bytesTotal"`
	LatencyMs                HistogramSnapshot  `json:"latencyMs"`
	SizeBytes                HistogramSnapshot  `json:"sizeBytes"`
	Throughput               ThroughputSnapshot `json:"throughput"`
}

func (s DirectionSnapshot) IsEmpty() bool {
	return s.OpCount == 0 && s.ErrorCount == 0 && s.MissCount == 0 && s.SkippedAlreadySavedCount == 0
}

type Snapshot struct {
	SchemaVersion int               `json:"schemaVersion"`
	Upload        DirectionSnapshot `json:"upload"`
	Download      DirectionSnapshot `json:"download"`
	// Optional breakdown for the tools with more than one protocol; ccache and Gradle omit it,
	// which is why the totals stay the top-level fields.
	CAS *ProtocolSnapshot `json:"cas,omitempty"`
	KV  *ProtocolSnapshot `json:"kv,omitempty"`
}

type ProtocolSnapshot struct {
	Upload   DirectionSnapshot `json:"upload"`
	Download DirectionSnapshot `json:"download"`
}

func (s Snapshot) IsEmpty() bool {
	return s.Upload.IsEmpty() && s.Download.IsEmpty()
}

// PercentileBucket answers "p50" without retained samples, as the bucket's upper boundary. The
// second result marks the unbounded top bucket, where that boundary is a floor not a ceiling.
func (h HistogramSnapshot) PercentileBucket(quantile float64) (int64, bool) {
	if h.Count == 0 || len(h.Boundaries) == 0 {
		return 0, false
	}

	rank := int64(math.Ceil(quantile * float64(h.Count)))
	if rank < 1 {
		rank = 1
	}

	var cumulative int64
	for i, count := range h.Counts {
		cumulative += count
		if cumulative >= rank {
			if i < len(h.Boundaries) {
				return h.Boundaries[i], false
			}

			return h.Boundaries[len(h.Boundaries)-1], true
		}
	}

	return h.Boundaries[len(h.Boundaries)-1], true
}

// ProfileLine is the operator-facing log line; latency and size read as bucket bounds because
// only throughput retains samples. Empty when nothing transferred, so the caller can skip it.
func (s DirectionSnapshot) ProfileLine() string {
	if s.OpCount == 0 {
		return ""
	}

	parts := []string{
		"latency " + percentilePair(s.LatencyMs, func(v int64) string { return fmt.Sprintf("%dms", v) }),
		"size " + percentilePair(s.SizeBytes, func(v int64) string { return humanize.Bytes(uint64(v)) }), //nolint:gosec // non-negative
	}

	if s.Throughput.Histogram.Count > 0 {
		parts = append(parts, fmt.Sprintf("throughput p50 %s/s, p90 %s/s",
			humanize.Bytes(uint64(s.Throughput.P50BytesPerSec)), //nolint:gosec // non-negative
			humanize.Bytes(uint64(s.Throughput.P90BytesPerSec)), //nolint:gosec // non-negative
		))
	} else {
		parts = append(parts, fmt.Sprintf("throughput n/a (all %d ops below the %s floor)",
			s.Throughput.ExcludedSmallOps,
			humanize.Bytes(uint64(s.Throughput.MinBlobBytes)))) //nolint:gosec // non-negative
	}

	return strings.Join(parts, " | ")
}

func percentilePair(h HistogramSnapshot, format func(int64) string) string {
	p50, over50 := h.PercentileBucket(0.50)
	p90, over90 := h.PercentileBucket(0.90)

	return fmt.Sprintf("p50 %s%s, p90 %s%s", boundPrefix(over50), format(p50), boundPrefix(over90), format(p90))
}

func boundPrefix(overflow bool) string {
	if overflow {
		return ">"
	}

	return "<="
}

type Collector struct {
	Upload   *Recorder
	Download *Recorder
}

func NewCollector() *Collector {
	return &Collector{
		Upload:   &Recorder{boundaries: defaultBoundaries()},
		Download: &Recorder{boundaries: defaultBoundaries()},
	}
}

// Safe on a nil Collector, so a caller that never wired one up still marshals.
func (c *Collector) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{SchemaVersion: SchemaVersion} //nolint:exhaustruct // zeroed directions
	}

	return Snapshot{
		SchemaVersion: SchemaVersion,
		Upload:        c.Upload.Snapshot(),
		Download:      c.Download.Snapshot(),
	}
}

// TakeSnapshot zeroes both directions, for a helper that serves several invocations in a row.
func (c *Collector) TakeSnapshot() Snapshot {
	if c == nil {
		return Snapshot{SchemaVersion: SchemaVersion} //nolint:exhaustruct // zeroed directions
	}

	snapshot := c.Snapshot()
	c.Upload.reset()
	c.Download.reset()

	return snapshot
}

// ProtocolCollector reports the totals as the union of CAS and KV, so the two cannot drift.
type ProtocolCollector struct {
	CAS *Collector
	KV  *Collector
}

func NewProtocolCollector() *ProtocolCollector {
	return &ProtocolCollector{CAS: NewCollector(), KV: NewCollector()}
}

func (c *ProtocolCollector) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{SchemaVersion: SchemaVersion} //nolint:exhaustruct // zeroed directions
	}

	cas, kv := c.CAS.Snapshot(), c.KV.Snapshot()

	return Snapshot{
		SchemaVersion: SchemaVersion,
		// From the retained samples, not the two snapshots: averaging would not be exact.
		Upload:   mergeDirections(c.CAS.Upload, c.KV.Upload),
		Download: mergeDirections(c.CAS.Download, c.KV.Download),
		CAS:      &ProtocolSnapshot{Upload: cas.Upload, Download: cas.Download},
		KV:       &ProtocolSnapshot{Upload: kv.Upload, Download: kv.Download},
	}
}

// One lock keeps a snapshot from catching the counters and the histograms disagreeing.
type Recorder struct {
	mu         sync.Mutex
	boundaries boundarySet

	latency    histogram
	size       histogram
	throughput histogram

	// Retained so the percentiles are exact rather than interpolated from the buckets.
	throughputSamples []int64

	opCount                  int64
	errorCount               int64
	missCount                int64
	skippedAlreadySavedCount int64
	bytesTotal               int64
	excludedSmallOps         int64
}

// RecordTransfer takes the wall clock of the whole operation, retries included.
func (r *Recorder) RecordTransfer(bytes int64, duration time.Duration) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.opCount++
	r.bytesTotal += bytes
	r.latency.record(r.boundaries.latencyMs, duration.Milliseconds())
	r.size.record(r.boundaries.sizeBytes, bytes)

	if bytes < ThroughputMinBlobBytes {
		r.excludedSmallOps++

		return
	}

	// Millisecond resolution, so a fast op can arrive as 0.
	elapsedMs := max(duration.Milliseconds(), 1)
	bytesPerSec := bytes * 1000 / elapsedMs

	r.throughput.record(r.boundaries.throughputBytesPerSec, bytesPerSec)
	if len(r.throughputSamples) < maxThroughputSamples {
		r.throughputSamples = append(r.throughputSamples, bytesPerSec)
	}
}

func (r *Recorder) RecordError() {
	r.addCounter(&r.errorCount)
}

func (r *Recorder) RecordMiss() {
	r.addCounter(&r.missCount)
}

// RecordSkippedAlreadySaved is uploads only: nothing went over the wire.
func (r *Recorder) RecordSkippedAlreadySaved() {
	r.addCounter(&r.skippedAlreadySavedCount)
}

func (r *Recorder) Snapshot() DirectionSnapshot {
	if r == nil {
		return DirectionSnapshot{} //nolint:exhaustruct // zero value is the empty snapshot
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	sorted := make([]int64, len(r.throughputSamples))
	copy(sorted, r.throughputSamples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	return DirectionSnapshot{
		OpCount:                  r.opCount,
		ErrorCount:               r.errorCount,
		MissCount:                r.missCount,
		SkippedAlreadySavedCount: r.skippedAlreadySavedCount,
		BytesTotal:               r.bytesTotal,
		LatencyMs:                r.latency.snapshot(r.boundaries.latencyMs),
		SizeBytes:                r.size.snapshot(r.boundaries.sizeBytes),
		Throughput: ThroughputSnapshot{
			Histogram:        r.throughput.snapshot(r.boundaries.throughputBytesPerSec),
			P10BytesPerSec:   percentile(sorted, 0.10),
			P50BytesPerSec:   percentile(sorted, 0.50),
			P90BytesPerSec:   percentile(sorted, 0.90),
			MinBlobBytes:     ThroughputMinBlobBytes,
			ExcludedSmallOps: r.excludedSmallOps,
		},
	}
}

// ---------------------------------------------------------------------------
// Private
// ---------------------------------------------------------------------------

// Locked CAS-first everywhere, so the two locks cannot be taken in opposing order.
func mergeDirections(first, second *Recorder) DirectionSnapshot {
	first.mu.Lock()
	defer first.mu.Unlock()
	second.mu.Lock()
	defer second.mu.Unlock()

	samples := make([]int64, 0, len(first.throughputSamples)+len(second.throughputSamples))
	samples = append(samples, first.throughputSamples...)
	samples = append(samples, second.throughputSamples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })

	boundaries := first.boundaries

	return DirectionSnapshot{
		OpCount:                  first.opCount + second.opCount,
		ErrorCount:               first.errorCount + second.errorCount,
		MissCount:                first.missCount + second.missCount,
		SkippedAlreadySavedCount: first.skippedAlreadySavedCount + second.skippedAlreadySavedCount,
		BytesTotal:               first.bytesTotal + second.bytesTotal,
		LatencyMs:                mergeHistograms(&first.latency, &second.latency, boundaries.latencyMs),
		SizeBytes:                mergeHistograms(&first.size, &second.size, boundaries.sizeBytes),
		Throughput: ThroughputSnapshot{
			Histogram: mergeHistograms(&first.throughput, &second.throughput,
				boundaries.throughputBytesPerSec),
			P10BytesPerSec:   percentile(samples, 0.10),
			P50BytesPerSec:   percentile(samples, 0.50),
			P90BytesPerSec:   percentile(samples, 0.90),
			MinBlobBytes:     ThroughputMinBlobBytes,
			ExcludedSmallOps: first.excludedSmallOps + second.excludedSmallOps,
		},
	}
}

func mergeHistograms(first, second *histogram, boundaries []int64) HistogramSnapshot {
	merged := histogram{ //nolint:exhaustruct // filled below
		counts: make([]int64, len(boundaries)+1),
		count:  first.count + second.count,
		sum:    first.sum + second.sum,
	}

	for i := range merged.counts {
		if i < len(first.counts) {
			merged.counts[i] += first.counts[i]
		}
		if i < len(second.counts) {
			merged.counts[i] += second.counts[i]
		}
	}

	switch {
	case first.count == 0:
		merged.minVal, merged.maxVal = second.minVal, second.maxVal
	case second.count == 0:
		merged.minVal, merged.maxVal = first.minVal, first.maxVal
	default:
		merged.minVal = min(first.minVal, second.minVal)
		merged.maxVal = max(first.maxVal, second.maxVal)
	}

	return merged.snapshot(boundaries)
}

type boundarySet struct {
	latencyMs             []int64
	sizeBytes             []int64
	throughputBytesPerSec []int64
}

func defaultBoundaries() boundarySet {
	return boundarySet{
		latencyMs:             LatencyMsBuckets,
		sizeBytes:             SizeBytesBuckets,
		throughputBytesPerSec: ThroughputBytesPerSecBuckets,
	}
}

func (r *Recorder) addCounter(counter *int64) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	*counter++
}

func (r *Recorder) reset() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.latency = histogram{}    //nolint:exhaustruct // zero value is the empty histogram
	r.size = histogram{}       //nolint:exhaustruct // zero value is the empty histogram
	r.throughput = histogram{} //nolint:exhaustruct // zero value is the empty histogram
	r.throughputSamples = nil
	r.opCount = 0
	r.errorCount = 0
	r.missCount = 0
	r.skippedAlreadySavedCount = 0
	r.bytesTotal = 0
	r.excludedSmallOps = 0
}

type histogram struct {
	counts []int64
	count  int64
	sum    int64
	minVal int64
	maxVal int64
}

func (h *histogram) record(boundaries []int64, value int64) {
	if h.counts == nil {
		h.counts = make([]int64, len(boundaries)+1)
	}

	h.counts[bucketOf(boundaries, value)]++
	h.count++
	h.sum += value

	if h.count == 1 {
		h.minVal, h.maxVal = value, value

		return
	}

	h.minVal = min(h.minVal, value)
	h.maxVal = max(h.maxVal, value)
}

func (h *histogram) snapshot(boundaries []int64) HistogramSnapshot {
	counts := make([]int64, len(boundaries)+1)
	copy(counts, h.counts)

	return HistogramSnapshot{
		Boundaries: boundaries,
		Counts:     counts,
		Count:      h.count,
		Sum:        h.sum,
		Min:        h.minVal,
		Max:        h.maxVal,
	}
}

// A nine-element scan avoids the floating point drift of a log at the boundaries.
func bucketOf(boundaries []int64, value int64) int {
	for i, boundary := range boundaries {
		if value <= boundary {
			return i
		}
	}

	return len(boundaries)
}

// percentile is nearest-rank, on an ascending slice.
func percentile(sorted []int64, quantile float64) int64 {
	if len(sorted) == 0 {
		return 0
	}

	rank := int(math.Ceil(quantile*float64(len(sorted)))) - 1

	return sorted[min(max(rank, 0), len(sorted)-1)]
}
