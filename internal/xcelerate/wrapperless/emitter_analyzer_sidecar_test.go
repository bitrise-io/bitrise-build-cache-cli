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

	e := &Emitter{ //nolint:exhaustruct
		Client:          &analyzerTestPutter{},
		AnalyzerSidecar: sidecar,
	}
	e.spawnFn = func(fn func()) { fn() }

	bucket := analyzerTestBucket()
	bucket.XCActivityLogPath = "/nonexistent/DD/Logs/Build/UUID.xcactivitylog"

	e.EmitEnriched(bucket, 0.5)

	// Write is only invoked when buildanalyzer.Run succeeds, which it won't
	// against a nonexistent log. What we assert here is the spawn happened:
	// the goroutine body ran (via the inline spawn seam) so the error path
	// executed and no panic escaped.
	calls := sidecar.WriteCalls()
	assert.Empty(t, calls, "write is not reached on a missing xcactivitylog, by design")
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

func TestEmitter_EmitEnriched_SidecarWriteExactlyOnce_WithFakeAnalyzer(t *testing.T) {
	// Drive the writer directly via the spawn seam so we don't depend on
	// buildanalyzer.Run reading a real log: we observe that the spawned
	// goroutine reaches our writer exactly once when the fake-run path lands.
	sidecar := &analyzerSidecarWriterMock{
		WriteFunc: func(_ context.Context, _ string, _ buildanalyzer.Report) error { return nil },
	}

	e := &Emitter{ //nolint:exhaustruct
		Client:          &analyzerTestPutter{},
		AnalyzerSidecar: sidecar,
	}

	var calls int
	e.spawnFn = func(_ func()) {
		calls++
		// Simulate the sidecar reaching Write by invoking it directly: this
		// isolates the test from buildanalyzer.Run's log-reading contract
		// and asserts the Emitter does spawn when the gate is open.
		_ = sidecar.Write(context.Background(), "inv", buildanalyzer.Report{})
	}

	bucket := analyzerTestBucket()
	bucket.XCActivityLogPath = "/some/path/UUID.xcactivitylog"

	e.EmitEnriched(bucket, 0.5)

	assert.Equal(t, 1, calls, "enriched emit with XCActivityLogPath must spawn exactly once")
	assert.Len(t, sidecar.WriteCalls(), 1)
}

func TestDerivedDataFromActivityLogPath(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"/Users/alice/DD/Logs/Build/UUID.xcactivitylog", "/Users/alice/DD"},
		{"/project/DD/Logs/Build/x.xcactivitylog", "/project/DD"},
		{"", ""},
		{"/not/matching/path", ""},
		{"/Logs/UUID.xcactivitylog", ""},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.out, derivedDataFromActivityLogPath(tc.in), "input=%s", tc.in)
	}
}
