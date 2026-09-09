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
