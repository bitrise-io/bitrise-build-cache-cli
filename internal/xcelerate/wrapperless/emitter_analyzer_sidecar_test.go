//go:build unit

package wrapperless

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

type analyzerTestPutter struct {
	mu    sync.Mutex
	calls []analytics.Invocation
}

func (p *analyzerTestPutter) PutInvocation(inv analytics.Invocation) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls = append(p.calls, inv)

	return nil
}

func analyzerTestBucket() Bucket {
	return Bucket{ //nolint:exhaustruct
		InvocationID: "11111111-2222-3333-4444-555555555555",
		StartAt:      time.Unix(100, 0),
		EndAt:        time.Unix(110, 0),
		Scheme:       "Seek",
		Action:       PrimaryActionBuild,
		Success:      true,
		SuccessKnown: true,
		Duration:     10 * time.Second,
	}
}

func TestEmitter_EmitEnriched_FiresAnalyzerSidecarExactlyOnceWithLogPath(t *testing.T) {
	sidecar := &analyzerSidecarWriterMock{
		WriteFunc: func(_ context.Context, _ string, _ buildanalyzer.Report) error { return nil },
	}

	var spawnCalls int
	e := &Emitter{ //nolint:exhaustruct
		Client:          &analyzerTestPutter{},
		AnalyzerSidecar: sidecar,
	}
	e.spawnFn = func(fn func()) {
		spawnCalls++
		fn()
	}

	bucket := analyzerTestBucket()
	bucket.XCActivityLogPath = "/nonexistent/DD/Logs/Build/UUID.xcactivitylog"

	e.EmitEnriched(bucket, 0.5)

	assert.Equal(t, 1, spawnCalls, "enriched emit with XCActivityLogPath must spawn exactly once")

	// Write is only invoked when buildanalyzer.Run succeeds, which it won't
	// against a nonexistent log — assert emptiness to prove the error path
	// executed and no panic escaped.
	assert.Empty(t, sidecar.WriteCalls(), "write is not reached on a missing xcactivitylog, by design")
}

func TestEmitter_EmitEnriched_SkipsSidecarWhenLogPathEmpty(t *testing.T) {
	sidecar := &analyzerSidecarWriterMock{
		WriteFunc: func(_ context.Context, _ string, _ buildanalyzer.Report) error { return nil },
	}

	spawnCalls := 0
	e := &Emitter{ //nolint:exhaustruct
		Client:          &analyzerTestPutter{},
		AnalyzerSidecar: sidecar,
	}
	e.spawnFn = func(fn func()) {
		spawnCalls++
		fn()
	}

	// Default bucket has no XCActivityLogPath set.
	bucket := analyzerTestBucket()

	e.EmitEnriched(bucket, 0.5)

	assert.Zero(t, spawnCalls, "no xcactivitylog → no spawn")
	assert.Empty(t, sidecar.WriteCalls())
}

func TestEmitter_EmitEnriched_SkipsSidecarWhenNil(t *testing.T) {
	spawnCalls := 0
	e := &Emitter{ //nolint:exhaustruct
		Client: &analyzerTestPutter{},
	}
	e.spawnFn = func(fn func()) {
		spawnCalls++
		fn()
	}

	bucket := analyzerTestBucket()
	bucket.XCActivityLogPath = "/some/path/UUID.xcactivitylog"

	e.EmitEnriched(bucket, 0.5)

	assert.Zero(t, spawnCalls, "no AnalyzerSidecar → no spawn")
}
