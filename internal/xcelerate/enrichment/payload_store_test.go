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
		Status:       enrichment.PayloadStatusPendingTerminal,
		Merged: enrichment.MergedPayload{
			Command: "build",
		},
	}))
	require.NoError(t, s.Upsert(enrichment.PayloadRecord{
		InvocationID: "id-2",
		Status:       enrichment.PayloadStatusPendingTerminal,
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
	assert.Equal(t, enrichment.PayloadStatusPendingTerminal, byID["id-2"].Status)
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
		`{"invocation_id":"ok","status":"pending_terminal","last_updated_at":"2026-11-06T10:00:00Z"}`+"\n"+
			`{not json}`+"\n"+
			`{"invocation_id":"","status":"pending_terminal"}`+"\n"+
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

	require.NoError(t, s.Upsert(enrichment.PayloadRecord{InvocationID: "keep", Status: enrichment.PayloadStatusPendingTerminal}))
	require.NoError(t, s.Upsert(enrichment.PayloadRecord{InvocationID: "drop", Status: enrichment.PayloadStatusPendingTerminal}))
	require.NoError(t, s.Upsert(enrichment.PayloadRecord{InvocationID: "drop", Status: enrichment.PayloadStatusEmitted}))

	require.NoError(t, s.Remove("drop"))
	records, err := s.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "keep", records[0].InvocationID)

	// Removing an absent ID is a no-op.
	require.NoError(t, s.Remove("absent"))
}

func TestPayloadStore_CompactCollapsesDuplicatesToOneLinePerID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.ndjson")
	s := &enrichment.PayloadStore{Path: path}

	for i := 0; i < 1000; i++ {
		require.NoError(t, s.Upsert(enrichment.PayloadRecord{
			InvocationID: "dup",
			Status:       enrichment.PayloadStatusPendingTerminal,
		}))
	}

	require.NoError(t, s.Compact())

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := 0
	for _, b := range body {
		if b == '\n' {
			lines++
		}
	}
	assert.Equal(t, 1, lines, "Compact must keep exactly one line per InvocationID")

	records, err := s.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "dup", records[0].InvocationID)
}

func TestPayloadStore_AutoCompactFiresPastThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.ndjson")
	// 1 KiB threshold forces compaction inside a short test loop.
	s := &enrichment.PayloadStore{Path: path, CompactThresholdBytes: 1 << 10}

	for i := 0; i < 200; i++ {
		require.NoError(t, s.Upsert(enrichment.PayloadRecord{
			InvocationID: "dup",
			Status:       enrichment.PayloadStatusPendingTerminal,
		}))
	}

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Less(t, info.Size(), int64(4<<10), "auto-compact must bound file size under light load")

	records, err := s.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)
}

func TestPayloadStore_UpsertStampsLastUpdatedAt(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.PayloadStore{Path: filepath.Join(dir, "payload.ndjson")}

	require.NoError(t, s.Upsert(enrichment.PayloadRecord{InvocationID: "id", Status: enrichment.PayloadStatusPendingTerminal}))
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

