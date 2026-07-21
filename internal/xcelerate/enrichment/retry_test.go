//go:build unit

package enrichment_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
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
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

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

	r := &enrichment.Retrier{Store: store, Client: mock, MaxAge: 24 * time.Hour}
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

func TestRetrier_StartupOrphanSweep_RemovesOldUntouched(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "old-orphan",
		StartTime:    now.Add(-48 * time.Hour),
	}))
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "fresh",
		StartTime:    now.Add(-time.Hour),
	}))

	r := &enrichment.Retrier{
		Store:    store,
		Client:   &InvocationPutterMock{},
		Interval: time.Hour,
		MaxAge:   24 * time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		loaded, err := store.Load()
		require.NoError(t, err)
		if len(loaded) == 1 && loaded[0].InvocationID == "fresh" {
			cancel()
			<-done

			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	<-done
	t.Fatal("startup orphan sweep did not remove old untouched record within 2s")
}

func TestRetrier_ConcurrentAppendAndSweep(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

	// Seed 5 retry records (Attempts > 0). Half will succeed, half will keep failing.
	const seeded = 5
	for i := 0; i < seeded; i++ {
		id := "seed-" + string(rune('a'+i))
		require.NoError(t, store.Append(enrichment.PendingRecord{
			InvocationID:    id,
			StartTime:       now.Add(-time.Minute),
			FirstAttempt:    now.Add(-time.Minute),
			LastAttempt:     now.Add(-time.Minute),
			Attempts:        1,
			EnrichedPayload: mustPayload(t, id),
		}))
	}

	var (
		putMu   sync.Mutex
		putSucc = map[string]bool{"seed-a": true, "seed-c": true, "seed-e": true}
	)
	mock := &InvocationPutterMock{PutInvocationFunc: func(inv analytics.Invocation) error {
		putMu.Lock()
		defer putMu.Unlock()
		if putSucc[inv.InvocationID] {
			return nil
		}

		return errors.New("still down")
	}}

	r := &enrichment.Retrier{Store: store, Client: mock, MaxAge: 24 * time.Hour, Now: func() time.Time { return now }}

	// Kick off Sweep + 10 concurrent Appends (Attempts=0 records — orphan-slim path).
	var wg sync.WaitGroup
	wg.Add(11)
	go func() {
		defer wg.Done()
		r.Sweep()
	}()

	const appended = 10
	for i := 0; i < appended; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("app-%d", i)
			require.NoError(t, store.Append(enrichment.PendingRecord{
				InvocationID: id,
				StartTime:    now,
			}))
		}()
	}

	wg.Wait()

	loaded, err := store.Load()
	require.NoError(t, err)

	// 3 seeded succeeded → dropped; 2 seeded failed → kept.
	// All 10 appended → kept.
	// Total: 12.
	assert.Len(t, loaded, appended+2, "concurrent Appends must not be dropped by a Sweep round; failed seeds must survive")

	seen := map[string]bool{}
	for _, r := range loaded {
		seen[r.InvocationID] = true
	}
	assert.True(t, seen["seed-b"], "failed retry must survive")
	assert.True(t, seen["seed-d"], "failed retry must survive")
	assert.False(t, seen["seed-a"], "successful retry must be dropped")
	assert.False(t, seen["seed-c"], "successful retry must be dropped")
	assert.False(t, seen["seed-e"], "successful retry must be dropped")
	for i := 0; i < appended; i++ {
		assert.True(t, seen[fmt.Sprintf("app-%d", i)], "concurrently appended record must survive")
	}
}
