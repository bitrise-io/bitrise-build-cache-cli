//go:build unit

package enrichment_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestPayloadStore_LoadMissingFileReturnsNil(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.PayloadStore{Path: filepath.Join(dir, "missing.ndjson")}

	records, err := s.Load()
	require.NoError(t, err)
	assert.Nil(t, records)
}

func TestPayloadStore_UpsertLoadRoundtripCollapsesDuplicates(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.PayloadStore{Path: filepath.Join(dir, "payload.ndjson")}

	require.NoError(t, s.Upsert(enrichment.PayloadRecord{
		InvocationID: "id-1",
		Status:       enrichment.PayloadStatusOpen,
		Merged: enrichment.MergedPayload{
			Command: "build",
		},
	}))
	require.NoError(t, s.Upsert(enrichment.PayloadRecord{
		InvocationID: "id-2",
		Status:       enrichment.PayloadStatusOpen,
	}))
	// Second upsert of id-1 — the loader must collapse to the newer one.
	require.NoError(t, s.Upsert(enrichment.PayloadRecord{
		InvocationID: "id-1",
		Status:       enrichment.PayloadStatusEmitted,
		Merged: enrichment.MergedPayload{
			Command: "build [Scheme]",
		},
	}))

	records, err := s.Load()
	require.NoError(t, err)
	require.Len(t, records, 2)

	byID := map[string]enrichment.PayloadRecord{}
	for _, r := range records {
		byID[r.InvocationID] = r
	}

	assert.Equal(t, enrichment.PayloadStatusEmitted, byID["id-1"].Status)
	assert.Equal(t, "build [Scheme]", byID["id-1"].Merged.Command)
	assert.Equal(t, enrichment.PayloadStatusOpen, byID["id-2"].Status)
}

func TestPayloadStore_UpsertRejectsEmptyID(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.PayloadStore{Path: filepath.Join(dir, "payload.ndjson")}

	err := s.Upsert(enrichment.PayloadRecord{})
	require.Error(t, err)
}

func TestPayloadStore_LoadSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.ndjson")
	require.NoError(t, os.WriteFile(path, []byte(
		`{"invocation_id":"ok","status":"open","last_updated_at":"2026-11-06T10:00:00Z"}`+"\n"+
			`{not json}`+"\n"+
			`{"invocation_id":"","status":"open"}`+"\n"+
			`{"invocation_id":"other","status":"emitted","last_updated_at":"2026-11-06T10:00:00Z"}`+"\n",
	), 0o644))

	s := &enrichment.PayloadStore{Path: path}
	records, err := s.Load()
	require.NoError(t, err)
	assert.Len(t, records, 2)
}

func TestPayloadStore_RemoveRewrites(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.PayloadStore{Path: filepath.Join(dir, "payload.ndjson")}

	require.NoError(t, s.Upsert(enrichment.PayloadRecord{InvocationID: "keep", Status: enrichment.PayloadStatusOpen}))
	require.NoError(t, s.Upsert(enrichment.PayloadRecord{InvocationID: "drop", Status: enrichment.PayloadStatusOpen}))
	require.NoError(t, s.Upsert(enrichment.PayloadRecord{InvocationID: "drop", Status: enrichment.PayloadStatusEmitted}))

	require.NoError(t, s.Remove("drop"))
	records, err := s.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "keep", records[0].InvocationID)

	// Removing an absent ID is a no-op.
	require.NoError(t, s.Remove("absent"))
}

func TestPayloadStore_UpsertStampsLastUpdatedAt(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.PayloadStore{Path: filepath.Join(dir, "payload.ndjson")}

	require.NoError(t, s.Upsert(enrichment.PayloadRecord{InvocationID: "id", Status: enrichment.PayloadStatusOpen}))
	records, err := s.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.False(t, records[0].LastUpdatedAt.IsZero(), "LastUpdatedAt must default to now")

	explicit := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	require.NoError(t, s.Upsert(enrichment.PayloadRecord{
		InvocationID:  "id",
		Status:        enrichment.PayloadStatusEmitted,
		LastUpdatedAt: explicit,
	}))
	records, err = s.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.True(t, records[0].LastUpdatedAt.Equal(explicit))
}

func TestMergeInto_UnionsSignals(t *testing.T) {
	dst := enrichment.MergedPayload{
		Command:      "build",
		Start:        time.Date(2026, 11, 6, 10, 0, 1, 0, time.UTC),
		Stop:         time.Date(2026, 11, 6, 10, 0, 5, 0, time.UTC),
		DurationMs:   4000,
		SuccessKnown: true,
		Success:      true,
	}

	src := enrichment.MergedPayload{
		Command:      "build [Scheme]",
		FullCommand:  "xcodebuild build -scheme Scheme",
		Scheme:       "Scheme",
		Action:       "build",
		Start:        time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), // earlier
		Stop:         time.Date(2026, 11, 6, 10, 0, 10, 0, time.UTC), // later
		DurationMs:   9000,
		HitRate:      0.75,
		HitRateKnown: true,
		LogOutcome:   "OK",
	}

	enrichment.MergeInto(&dst, src)

	assert.Equal(t, "build [Scheme]", dst.Command)
	assert.Equal(t, "xcodebuild build -scheme Scheme", dst.FullCommand)
	assert.Equal(t, "Scheme", dst.Scheme)
	assert.True(t, dst.Start.Equal(time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)))
	assert.True(t, dst.Stop.Equal(time.Date(2026, 11, 6, 10, 0, 10, 0, time.UTC)))
	assert.Equal(t, int64(9000), dst.DurationMs)
	assert.Equal(t, float32(0.75), dst.HitRate)
	assert.True(t, dst.HitRateKnown)
	assert.Equal(t, "OK", dst.LogOutcome)
	assert.True(t, dst.SuccessKnown)
	assert.True(t, dst.Success)
}
