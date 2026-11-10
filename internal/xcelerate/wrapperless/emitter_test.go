//go:build unit

package wrapperless_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/wrapperless"
)

type recordingPutter struct {
	mu    sync.Mutex
	calls []analytics.Invocation
	fails int
}

func (r *recordingPutter) PutInvocation(inv analytics.Invocation) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, inv)
	if r.fails > 0 {
		r.fails--

		return errors.New("transient")
	}

	return nil
}

func (r *recordingPutter) snapshot() []analytics.Invocation {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]analytics.Invocation, len(r.calls))
	copy(out, r.calls)

	return out
}

func newBucket() wrapperless.Bucket {
	return wrapperless.Bucket{
		InvocationID: "11111111-2222-3333-4444-555555555555",
		StartAt:      time.Unix(100, 0),
		EndAt:        time.Unix(110, 0),
		Scheme:       "Seek",
		Action:       wrapperless.PrimaryActionBuild,
		Success:      true,
		SuccessKnown: true,
		Duration:     10 * time.Second,
		BlobStats: &blobstats.Snapshot{ //nolint:exhaustruct
			Download: blobstats.DirectionSnapshot{OpCount: 1}, //nolint:exhaustruct
		},
	}
}

func TestEmitter_SlimThenEnrichedSameID(t *testing.T) {
	putter := &recordingPutter{}
	e := &wrapperless.Emitter{
		Client:          putter,
		SlimBackoff:     time.Millisecond,
		SlimRetryBudget: 1,
	}

	b := newBucket()
	e.EmitSlim(b)
	e.EmitEnriched(b, 0.75)

	calls := putter.snapshot()
	require.Len(t, calls, 2)
	assert.Equal(t, b.InvocationID, calls[0].InvocationID)
	assert.Equal(t, b.InvocationID, calls[1].InvocationID)
	assert.Equal(t, "build [Seek]", calls[0].Command)
	assert.Zero(t, calls[0].HitRate)
	assert.Nil(t, calls[0].CacheBlobStats)
	assert.InDelta(t, 0.75, calls[1].HitRate, 0.0001)
	assert.NotNil(t, calls[1].CacheBlobStats)
}

func TestEmitter_SlimRetriesOnFailure(t *testing.T) {
	putter := &recordingPutter{fails: 2}
	e := &wrapperless.Emitter{
		Client:          putter,
		SlimBackoff:     time.Millisecond,
		SlimRetryBudget: 5,
	}

	e.EmitSlim(newBucket())

	calls := putter.snapshot()
	assert.Len(t, calls, 3)
}

func TestEmitter_SlimPersistsOnExhausted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending-slim.ndjson")

	putter := &recordingPutter{fails: 10}
	e := &wrapperless.Emitter{
		Client:          putter,
		SlimBackoff:     time.Millisecond,
		SlimRetryBudget: 2,
		PendingSlimPath: path,
	}

	b := newBucket()
	e.EmitSlim(b)

	body, err := os.ReadFile(path) //nolint:gosec // test file
	require.NoError(t, err)

	var p struct {
		Phase  wrapperless.Phase
		Bucket wrapperless.Bucket
	}
	require.NoError(t, json.Unmarshal(body, &p))

	assert.Equal(t, wrapperless.PhaseSlim, p.Phase)
	assert.Equal(t, b.InvocationID, p.Bucket.InvocationID)
}

func TestEmitter_NilClientIsNoop(t *testing.T) {
	e := &wrapperless.Emitter{}
	e.EmitSlim(newBucket())
	e.EmitEnriched(newBucket(), 1.0)
}

type recordingAppender struct {
	mu      sync.Mutex
	records []invocations.Record
}

func (r *recordingAppender) Append(rec invocations.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.records = append(r.records, rec)

	return nil
}

func (r *recordingAppender) snapshot() []invocations.Record {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]invocations.Record, len(r.records))
	copy(out, r.records)

	return out
}

func TestEmitter_EmitEnriched_TicksHealthOnSuccess(t *testing.T) {
	hw := &enrichment.HealthWriter{Path: filepath.Join(t.TempDir(), "health.json")}
	e := &wrapperless.Emitter{
		Client: &recordingPutter{},
		Health: hw,
	}

	e.EmitEnriched(newBucket(), 0.5)

	snap, err := enrichment.LoadHealth(hw.Path)
	require.NoError(t, err)
	assert.False(t, snap.LastAttempt.IsZero(), "TickAttempt must land")
	assert.False(t, snap.LastSuccess.IsZero(), "TickSuccess must land on 200")
	assert.False(t, snap.LastMatched.IsZero(), "wrapperless ticks matched=true (correlation-free)")
	assert.Zero(t, snap.ConsecutiveErrors)
}

type failingPutter struct{}

func (failingPutter) PutInvocation(_ analytics.Invocation) error {
	return errors.New("500 internal")
}

func TestEmitter_EmitEnriched_TicksHealthOnFailure(t *testing.T) {
	hw := &enrichment.HealthWriter{Path: filepath.Join(t.TempDir(), "health.json")}
	e := &wrapperless.Emitter{
		Client: failingPutter{},
		Health: hw,
	}

	e.EmitEnriched(newBucket(), 0.5)

	snap, err := enrichment.LoadHealth(hw.Path)
	require.NoError(t, err)
	assert.False(t, snap.LastAttempt.IsZero())
	assert.True(t, snap.LastSuccess.IsZero(), "TickSuccess must not fire on failure")
	assert.Equal(t, 1, snap.ConsecutiveErrors)
	assert.Equal(t, "500 internal", snap.LastError)
}

func TestEmitter_EmitEnriched_AppendsLocalLog(t *testing.T) {
	appender := &recordingAppender{}
	e := &wrapperless.Emitter{
		Client:           &recordingPutter{},
		LocalLogAppender: appender,
		XcodeVersion:     "16.0",
	}

	b := newBucket()
	e.EmitEnriched(b, 0.75)

	recs := appender.snapshot()
	require.Len(t, recs, 1, "successful enriched PUT must append exactly one local-log record")

	rec := recs[0]
	assert.Equal(t, b.InvocationID, rec.InvocationID)
	assert.Equal(t, "build [Seek]", rec.Command)
	assert.Equal(t, invocations.ToolXcode, rec.Tool)
	assert.Equal(t, "16.0", rec.ToolVersion)
	assert.InDelta(t, float32(0.75), rec.HitRate, 0.0001)
	assert.Equal(t, 0, rec.ExitCode, "SuccessKnown=true + Success=true → exit 0")
}

func TestEmitter_EmitEnriched_LocalLogOmittedOnFailure(t *testing.T) {
	appender := &recordingAppender{}
	e := &wrapperless.Emitter{
		Client:           failingPutter{},
		LocalLogAppender: appender,
	}

	e.EmitEnriched(newBucket(), 0.5)

	assert.Empty(t, appender.snapshot(), "failed PUT must not reach the local log")
}
