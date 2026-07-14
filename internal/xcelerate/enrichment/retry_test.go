//go:build unit

package enrichment_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func mustPayload(t *testing.T, id string) json.RawMessage {
	t.Helper()

	b, err := json.Marshal(analytics.Invocation{InvocationID: id})
	require.NoError(t, err)

	return b
}

func TestRetrier_SkipsUntouchedRecords(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "unretried",
		StartTime:    time.Now(),
	}))

	mock := &InvocationPutterMock{PutInvocationFunc: func(_ analytics.Invocation) error {
		t.Fatal("PutInvocation must not be called for un-attempted records")

		return nil
	}}

	r := &enrichment.Retrier{Store: store, Client: mock, MaxAge: 24 * time.Hour, Now: time.Now}
	r.Sweep()

	loaded, err := store.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "unretried", loaded[0].InvocationID)
}

func TestRetrier_RetriesFailedRecord(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID:    "will-retry",
		StartTime:       now,
		Attempts:        1,
		FirstAttempt:    now,
		LastAttempt:     now,
		LastError:       "boom",
		EnrichedPayload: mustPayload(t, "will-retry"),
	}))

	mock := &InvocationPutterMock{PutInvocationFunc: func(_ analytics.Invocation) error { return errors.New("still down") }}

	r := &enrichment.Retrier{Store: store, Client: mock, MaxAge: 24 * time.Hour, Now: func() time.Time { return now.Add(time.Minute) }}
	r.Sweep()

	loaded, err := store.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "will-retry", loaded[0].InvocationID)
	assert.Equal(t, 2, loaded[0].Attempts)
	assert.Contains(t, loaded[0].LastError, "still down")
}

func TestRetrier_GivesUpAfterMaxAge(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson"), Now: func() time.Time {
		return time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	}}

	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC) // 24h after FirstAttempt
	firstAttempt := now.Add(-25 * time.Hour)

	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID:    "expired",
		StartTime:       firstAttempt,
		Attempts:        5,
		FirstAttempt:    firstAttempt,
		LastAttempt:     now.Add(-time.Hour),
		EnrichedPayload: mustPayload(t, "expired"),
	}))

	mock := &InvocationPutterMock{PutInvocationFunc: func(_ analytics.Invocation) error {
		t.Fatal("PutInvocation must not fire for expired records")

		return nil
	}}

	r := &enrichment.Retrier{Store: store, Client: mock, MaxAge: 24 * time.Hour, Now: func() time.Time { return now }}
	r.Sweep()

	loaded, err := store.Load()
	require.NoError(t, err)
	assert.Empty(t, loaded, "expired record must be dropped")
}

func TestRetrier_RemovesRecordOnSuccess(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID:    "will-succeed",
		StartTime:       now,
		Attempts:        1,
		FirstAttempt:    now,
		LastAttempt:     now,
		EnrichedPayload: mustPayload(t, "will-succeed"),
	}))

	mock := &InvocationPutterMock{PutInvocationFunc: func(_ analytics.Invocation) error { return nil }}

	r := &enrichment.Retrier{Store: store, Client: mock, MaxAge: 24 * time.Hour, Now: func() time.Time { return now.Add(time.Minute) }}
	r.Sweep()

	loaded, err := store.Load()
	require.NoError(t, err)
	assert.Empty(t, loaded, "successful retry must remove the record")
}

func TestRetrier_SweepClosesOnCtxCancel(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	mock := &InvocationPutterMock{}

	r := &enrichment.Retrier{
		Store:    store,
		Client:   mock,
		Interval: 10 * time.Millisecond,
		MaxAge:   24 * time.Hour,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Retrier.Run did not exit within 1s of ctx cancel")
	}
}
