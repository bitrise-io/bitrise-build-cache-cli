//go:build unit

package blobstats_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

func Test_Recorder_emptySnapshotIsEmpty(t *testing.T) {
	snapshot := blobstats.NewCollector().Snapshot()

	assert.True(t, snapshot.IsEmpty())
	assert.Equal(t, blobstats.SchemaVersion, snapshot.SchemaVersion)
	assert.Len(t, snapshot.Download.LatencyMs.Counts, len(blobstats.LatencyMsBuckets)+1)
	assert.Zero(t, snapshot.Download.LatencyMs.Min)
	assert.Zero(t, snapshot.Download.LatencyMs.Max)
}

func Test_Recorder_valueEqualToBoundaryStaysInThatBucket(t *testing.T) {
	c := blobstats.NewCollector()

	// 8 ms is the boundary of bucket index 3 ({1,2,4,8,...}).
	c.Download.RecordTransfer(1024, 8*time.Millisecond)

	h := c.Snapshot().Download.LatencyMs
	assert.Equal(t, int64(1), h.Counts[3])
	assert.Equal(t, int64(1), h.Count)
	assert.Equal(t, int64(8), h.Sum)
}

func Test_Recorder_overflowBucketKeepsSumMinMax(t *testing.T) {
	c := blobstats.NewCollector()

	c.Download.RecordTransfer(1024, 5*time.Millisecond)
	c.Download.RecordTransfer(1024, 10*time.Second)

	h := c.Snapshot().Download.LatencyMs
	require.Len(t, h.Counts, len(blobstats.LatencyMsBuckets)+1)
	assert.Equal(t, int64(1), h.Counts[len(h.Counts)-1], "10s lands in the overflow bucket")
	assert.Equal(t, int64(2), h.Count)
	assert.Equal(t, int64(10_005), h.Sum)
	assert.Equal(t, int64(5), h.Min)
	assert.Equal(t, int64(10_000), h.Max)
}

func Test_Recorder_throughputFloorExcludesSmallBlobs(t *testing.T) {
	c := blobstats.NewCollector()

	c.Download.RecordTransfer(blobstats.ThroughputMinBlobBytes-1, 10*time.Millisecond)
	c.Download.RecordTransfer(blobstats.ThroughputMinBlobBytes, 10*time.Millisecond)

	got := c.Snapshot().Download
	assert.Equal(t, int64(2), got.OpCount, "both count as transfers")
	assert.Equal(t, int64(2), got.LatencyMs.Count)
	assert.Equal(t, int64(2), got.SizeBytes.Count)
	assert.Equal(t, int64(1), got.Throughput.ExcludedSmallOps)
	assert.Equal(t, int64(1), got.Throughput.Histogram.Count)
	assert.Equal(t, int64(blobstats.ThroughputMinBlobBytes), got.Throughput.MinBlobBytes)
}

func Test_Recorder_percentilesAreOrdered(t *testing.T) {
	c := blobstats.NewCollector()

	// 100 ops of steadily rising throughput: 1 MB in 100ms down to 1 MB in 1ms.
	for i := 100; i >= 1; i-- {
		c.Download.RecordTransfer(1_048_576, time.Duration(i)*time.Millisecond)
	}

	tp := c.Snapshot().Download.Throughput
	assert.Less(t, tp.P10BytesPerSec, tp.P50BytesPerSec)
	assert.Less(t, tp.P50BytesPerSec, tp.P90BytesPerSec)
}

func Test_Recorder_subMillisecondOpDoesNotDivideByZero(t *testing.T) {
	c := blobstats.NewCollector()

	c.Upload.RecordTransfer(1_048_576, 0)

	tp := c.Snapshot().Upload.Throughput
	assert.Equal(t, int64(1_048_576_000), tp.P50BytesPerSec, "0ms is floored to 1ms")
}

func Test_Recorder_missesAndErrorsAreCountedNotTimed(t *testing.T) {
	c := blobstats.NewCollector()

	c.Download.RecordMiss()
	c.Download.RecordError()
	c.Upload.RecordSkippedAlreadySaved()

	snapshot := c.Snapshot()
	assert.False(t, snapshot.IsEmpty())
	assert.Equal(t, int64(1), snapshot.Download.MissCount)
	assert.Equal(t, int64(1), snapshot.Download.ErrorCount)
	assert.Zero(t, snapshot.Download.OpCount)
	assert.Zero(t, snapshot.Download.LatencyMs.Count)
	assert.Equal(t, int64(1), snapshot.Upload.SkippedAlreadySavedCount)
}

func Test_Collector_takeSnapshotResetsForTheNextInvocation(t *testing.T) {
	c := blobstats.NewCollector()
	c.Download.RecordTransfer(32_768, 4*time.Millisecond)

	first := c.TakeSnapshot()
	require.Equal(t, int64(1), first.Download.OpCount)

	assert.True(t, c.Snapshot().IsEmpty())
}

func Test_Collector_nilIsSafe(t *testing.T) {
	var c *blobstats.Collector

	assert.True(t, c.Snapshot().IsEmpty())
	assert.NotPanics(t, func() { c.Snapshot().Download.IsEmpty() })
}

func Test_Snapshot_roundTripsThroughJSONAndProto(t *testing.T) {
	c := blobstats.NewCollector()
	c.Download.RecordTransfer(2_097_152, 40*time.Millisecond)
	c.Download.RecordMiss()
	c.Upload.RecordTransfer(512, 1*time.Millisecond)
	c.Upload.RecordSkippedAlreadySaved()
	original := c.Snapshot()

	payload, err := json.Marshal(original)
	require.NoError(t, err)

	var viaJSON blobstats.Snapshot
	require.NoError(t, json.Unmarshal(payload, &viaJSON))
	assert.Equal(t, original, viaJSON)

	viaProto := blobstats.FromProto(blobstats.ToProto(original))
	require.NotNil(t, viaProto)
	assert.Equal(t, original, *viaProto)
}

func Test_ToProto_isNilForAnEmptySession(t *testing.T) {
	assert.Nil(t, blobstats.ToProto(blobstats.NewCollector().Snapshot()))
	assert.Nil(t, blobstats.FromProto(nil))
}

func Test_Recorder_measuredDistributionLandsInTheExpectedBuckets(t *testing.T) {
	c := blobstats.NewCollector()

	// One blob per size bucket boundary, all at 1 ms.
	for _, boundary := range blobstats.SizeBytesBuckets {
		c.Download.RecordTransfer(boundary, time.Millisecond)
	}

	got := c.Snapshot().Download
	assert.Equal(t, int64(len(blobstats.SizeBytesBuckets)), got.OpCount)
	for i := range blobstats.SizeBytesBuckets {
		assert.Equal(t, int64(1), got.SizeBytes.Counts[i], "bucket %d", i)
	}
	assert.Zero(t, got.SizeBytes.Counts[len(got.SizeBytes.Counts)-1], "nothing overflows")
	assert.Equal(t, int64(len(blobstats.SizeBytesBuckets)), got.LatencyMs.Counts[0])
}

func Test_ProtocolCollector_totalsAreTheUnionOfTheLanes(t *testing.T) {
	c := blobstats.NewProtocolCollector()

	c.CAS.Download.RecordTransfer(64*1024, 4*time.Millisecond)
	c.CAS.Download.RecordTransfer(128*1024, 8*time.Millisecond)
	c.CAS.Download.RecordMiss()
	c.CAS.Upload.RecordTransfer(32*1024, 2*time.Millisecond)
	c.CAS.Upload.RecordSkippedAlreadySaved()
	c.KV.Download.RecordTransfer(256*1024, 16*time.Millisecond)
	c.KV.Download.RecordError()
	c.KV.Upload.RecordTransfer(512*1024, 32*time.Millisecond)

	got := c.Snapshot()
	require.NotNil(t, got.CAS)
	require.NotNil(t, got.KV)

	assert.Equal(t, int64(3), got.Download.OpCount, "2 CAS + 1 KV")
	assert.Equal(t, int64(1), got.Download.MissCount)
	assert.Equal(t, int64(1), got.Download.ErrorCount)
	assert.Equal(t, int64((64+128+256)*1024), got.Download.BytesTotal)
	assert.Equal(t, int64(2), got.Upload.OpCount)
	assert.Equal(t, int64(1), got.Upload.SkippedAlreadySavedCount)
	assert.Equal(t, int64((32+512)*1024), got.Upload.BytesTotal)

	// Every total is the sum of the two lanes, so a consumer of the totals cannot see them
	// disagree with the breakdown.
	assert.Equal(t, got.CAS.Download.OpCount+got.KV.Download.OpCount, got.Download.OpCount)
	assert.Equal(t, got.CAS.Upload.BytesTotal+got.KV.Upload.BytesTotal, got.Upload.BytesTotal)
	assert.Equal(t, got.CAS.Download.LatencyMs.Sum+got.KV.Download.LatencyMs.Sum, got.Download.LatencyMs.Sum)
	assert.Equal(t, int64(4), got.Download.LatencyMs.Min, "the lower of the two lanes")
	assert.Equal(t, int64(16), got.Download.LatencyMs.Max, "the higher of the two lanes")

	for i := range got.Download.LatencyMs.Counts {
		assert.Equal(t,
			got.CAS.Download.LatencyMs.Counts[i]+got.KV.Download.LatencyMs.Counts[i],
			got.Download.LatencyMs.Counts[i], "latency bucket %d", i)
	}
}

// The merged percentiles come from the concatenated samples, not from averaging the lanes'
// percentiles, so they stay exact.
func Test_ProtocolCollector_mergedPercentilesAreExact(t *testing.T) {
	c := blobstats.NewProtocolCollector()

	// One slow CAS op against nine fast KV ops. Each lane's own median is at one extreme, so
	// averaging them would give ~505 MB/s — a value no sample has.
	c.CAS.Download.RecordTransfer(1_048_576, 100*time.Millisecond)
	for range 9 {
		c.KV.Download.RecordTransfer(1_048_576, time.Millisecond)
	}

	got := c.Snapshot()

	const slow, fast = int64(10_485_760), int64(1_048_576_000)
	require.Equal(t, slow, got.CAS.Download.Throughput.P50BytesPerSec)
	require.Equal(t, fast, got.KV.Download.Throughput.P50BytesPerSec)

	// Nearest-rank over the union of the ten samples: the 5th ascending is a fast one.
	assert.Equal(t, fast, got.Download.Throughput.P50BytesPerSec)
	assert.NotEqual(t, (slow+fast)/2, got.Download.Throughput.P50BytesPerSec,
		"a merged percentile must not be the mean of the lanes' percentiles")
	// The slowest op is the 1st ascending, so p10 is where it lands.
	assert.Equal(t, slow, got.Download.Throughput.P10BytesPerSec)
	assert.Equal(t, int64(10), got.Download.Throughput.Histogram.Count)
}

func Test_ProtocolCollector_emptyLanesStayEmpty(t *testing.T) {
	got := blobstats.NewProtocolCollector().Snapshot()

	assert.True(t, got.IsEmpty())
	assert.Nil(t, blobstats.ToProto(got))
}

// A plain collector sends no breakdown, which is the shape the Gradle plugin also sends.
func Test_Collector_omitsTheProtocolBreakdown(t *testing.T) {
	c := blobstats.NewCollector()
	c.Download.RecordTransfer(1024, time.Millisecond)

	got := c.Snapshot()
	assert.Nil(t, got.CAS)
	assert.Nil(t, got.KV)

	payload, err := json.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(payload), `"cas"`)
	assert.NotContains(t, string(payload), `"kv"`)
}
