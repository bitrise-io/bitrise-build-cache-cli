//go:build unit

package enrichment_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestEnricher_EmitOrphanPUTsUnderProxyInvocationID(t *testing.T) {
	dir := t.TempDir()
	pstore := &enrichment.PayloadStore{Path: filepath.Join(dir, "payload.ndjson")}

	// Place a sidecar file so EmitOrphan's successful PUT can unlink it.
	sidecarPath := filepath.Join(dir, "sidecar.json")
	require.NoError(t, os.WriteFile(sidecarPath, []byte(`{}`), 0o600))

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Client: mock, PayloadStore: pstore}
	e.EmitOrphan(&enrichment.ProxyIDGroup{
		InvocationID:    "orphan-id",
		Primary:         enrichment.RowDarkBuild,
		Start:           time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC),
		Stop:            time.Date(2026, 11, 6, 10, 0, 5, 0, time.UTC),
		SidecarPaths:    []string{sidecarPath},
		DerivedDataPath: "/x/Library/Developer/Xcode/DerivedData/Seek-abcdefghijklmnopqrstuv",
	})

	require.Len(t, mock.PutInvocationCalls(), 1, "orphan emit must PUT exactly once")
	assert.Equal(t, "orphan-id", captured.InvocationID)
	assert.Contains(t, captured.Command, "build", "DarkBuild RowType → build label")
	assert.Contains(t, captured.Command, "Seek", "scheme from DD basename rides into command label")

	records, err := pstore.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, enrichment.PayloadStatusEmitted, records[0].Status)

	_, statErr := os.Stat(sidecarPath)
	assert.True(t, os.IsNotExist(statErr), "sidecar must be unlinked on successful PUT")
}

func TestEnricher_EmitCrashFallbackRePUTsPendingRecord(t *testing.T) {
	dir := t.TempDir()
	pstore := &enrichment.PayloadStore{Path: filepath.Join(dir, "payload.ndjson")}

	// Seed a PendingTerminal record — the proxy-ID path's analog of a Pending
	// signal held at the single-ID ledger before any crash fires.
	require.NoError(t, pstore.Upsert(enrichment.PayloadRecord{
		InvocationID: "crash-id",
		Status:       enrichment.PayloadStatusPendingTerminal,
		Merged: enrichment.MergedPayload{
			Command:     "build [Scheme]",
			FullCommand: "xcodebuild build -scheme Scheme",
			Scheme:      "Scheme",
			Start:       time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC),
			Stop:        time.Date(2026, 11, 6, 10, 0, 42, 0, time.UTC),
			DurationMs:  42_000,
		},
	}))

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Client: mock, PayloadStore: pstore}
	e.EmitCrashFallback("crash-id")

	assert.Equal(t, 1, len(mock.PutInvocationCalls()), "crash-fallback must PUT exactly once")
	assert.Equal(t, "crash-id", captured.InvocationID)
	assert.Equal(t, "build [Scheme]", captured.Command)
	assert.Equal(t, int64(42_000), captured.DurationMs)
	assert.False(t, captured.Success, "crash-fallback stands for an unobserved terminal → not success")

	records, err := pstore.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, enrichment.PayloadStatusEmitted, records[0].Status, "PUT success flips status to Emitted")
}

func TestEnricher_EmitCrashFallbackSkipsAlreadyEmitted(t *testing.T) {
	dir := t.TempDir()
	pstore := &enrichment.PayloadStore{Path: filepath.Join(dir, "payload.ndjson")}

	require.NoError(t, pstore.Upsert(enrichment.PayloadRecord{
		InvocationID: "already-done",
		Status:       enrichment.PayloadStatusEmitted,
	}))

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return nil },
	}
	e := &enrichment.Enricher{Client: mock, PayloadStore: pstore}
	e.EmitCrashFallback("already-done")

	assert.Empty(t, mock.PutInvocationCalls(), "Emitted records must not re-emit")
}

func TestEnricher_PayloadStoreUpsertedOnSuccessfulPUT(t *testing.T) {
	dir := t.TempDir()
	pstore := &enrichment.PayloadStore{Path: filepath.Join(dir, "payload.ndjson")}

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return nil },
	}

	e := &enrichment.Enricher{Client: mock, PayloadStore: pstore}

	manifestPath, logName := stageOKLog(t)
	entry := enrichment.ManifestEntry{
		UUID:      "manifest-xyz",
		Signature: "Build MyScheme",
		FileName:  logName,
		Start:     time.Now(),
		Stop:      time.Now().Add(2 * time.Second),
	}
	e.Enrich(manifestPath, singleEntryGroup(entry))

	records, err := pstore.Load()
	require.NoError(t, err)
	require.Len(t, records, 1, "exactly one payload record must land for the PUT")
	assert.Equal(t, "manifest-xyz", records[0].InvocationID)
	assert.Equal(t, enrichment.PayloadStatusEmitted, records[0].Status)
	assert.Equal(t, "ok", records[0].Merged.LogOutcome)
}
