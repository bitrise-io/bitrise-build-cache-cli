//go:build unit

package wrapperless

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestEmitter_EmitEnriched_SidecarReceivesSchemeAndCommandFromBucket(t *testing.T) {
	sidecar := &analyzerSidecarWriterMock{
		WriteFunc: func(_ context.Context, _ string, _ buildanalyzer.Report) error { return nil },
	}

	var (
		gotScheme  string
		gotCommand string
	)

	e := &Emitter{ //nolint:exhaustruct
		Client:          &analyzerTestPutter{},
		AnalyzerSidecar: sidecar,
	}
	e.spawnFn = func(fn func()) { fn() }
	e.analyzerRunFn = func(_ context.Context, _ string, opts buildanalyzer.RunOptions) (buildanalyzer.Report, []error, error) {
		gotScheme = opts.Scheme
		gotCommand = opts.Command

		return buildanalyzer.Report{
			Build: buildanalyzer.BuildMeta{Scheme: opts.Scheme, Command: opts.Command},
		}, nil, nil
	}

	bucket := analyzerTestBucket()
	bucket.XCActivityLogPath = "/nonexistent/DD/Logs/Build/UUID.xcactivitylog"

	e.EmitEnriched(bucket, 0.5)

	assert.Equal(t, "Seek", gotScheme, "scheme must mirror bucket.Scheme")
	assert.Equal(t, "build [Seek]", gotCommand, "command must be the inv.Command computed by shortCommand(bucket)")

	writes := sidecar.WriteCalls()
	require.Len(t, writes, 1)
	assert.Equal(t, "Seek", writes[0].Rep.Build.Scheme)
	assert.Equal(t, "build [Seek]", writes[0].Rep.Build.Command)
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
