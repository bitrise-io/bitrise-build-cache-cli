//go:build unit

package xcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs/mocks"
)

type fakeXcodeRunner struct {
	stats xcodeargs.RunStats
}

func (f *fakeXcodeRunner) Run(_ context.Context, _ []string) xcodeargs.RunStats {
	return f.stats
}

func runnerForLocalLogTest(t *testing.T, runStats xcodeargs.RunStats, ciProvider string) (*XcodebuildRunner, *localInvocationLoggerMock) {
	t.Helper()

	logMock := &utilsMocks.Logger{}
	for _, name := range []string{"TDebugf", "TInfof", "TDonef", "TErrorf", "Errorf", "Warnf", "Debugf", "Infof"} {
		logMock.On(name, mock.Anything).Return()
		logMock.On(name, mock.Anything, mock.Anything).Return()
		logMock.On(name, mock.Anything, mock.Anything, mock.Anything).Return()
		logMock.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	}

	xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
		HasBuildActionFunc: func() bool { return true },
		ArgsFunc:           func(_ map[string]string) []string { return nil },
		CommandFunc:        func() string { return "xcodebuild -workspace Foo.xcworkspace" },
		ShortCommandFunc:   func() string { return "xcodebuild" },
	}
	xcodeRunner := &fakeXcodeRunner{stats: runStats}

	localLogger := &localInvocationLoggerMock{
		AppendFunc: func(invocations.Record) error { return nil },
	}

	return &XcodebuildRunner{
		Config:        xcelerate.Config{},
		Metadata:      common.CacheConfigMetadata{CIProvider: ciProvider, CLIVersion: "v2.8.6"},
		InvocationID:  "test-inv",
		Logger:        logMock,
		CacheLogger:   logMock,
		XcodeRunner:   xcodeRunner,
		XcodeArgs:     &xcodeArgProvider,
		invocationAPI: &stubInvocationSaver{},
		relationAPI:   &relationSenderMock{},
		localLogger:   localLogger,
	}, localLogger
}

func TestXcodebuildRunner_appendLocalInvocationLog_writesRecord(t *testing.T) {
	start := time.Date(2026, 6, 25, 13, 14, 15, 0, time.UTC)
	runStats := xcodeargs.RunStats{
		StartTime:        start,
		DurationMS:       1500,
		ExitCode:         0,
		Success:          true,
		XcodeVersion:     "16.0",
		XcodeBuildNumber: "16A123",
		CacheStats:       xcodeargs.CompCacheStats{Hits: 3, TotalTasks: 4},
	}

	sut, logger := runnerForLocalLogTest(t, runStats, "")

	_ = sut.Run(context.Background())

	calls := logger.AppendCalls()
	require.Len(t, calls, 1)

	got := calls[0].Rec
	assert.Equal(t, "test-inv", got.InvocationID)
	assert.Equal(t, invocations.ToolXcode, got.Tool)
	assert.Equal(t, "16.0", got.ToolVersion)
	assert.Equal(t, "v2.8.6", got.CLIVersion)
	assert.Equal(t, start, got.StartedAt)
	assert.Equal(t, start.Add(1500*time.Millisecond), got.FinishedAt)
	assert.Equal(t, 0, got.ExitCode)
	assert.Empty(t, got.CIProvider)
	assert.True(t, got.IsLocal())
	assert.InDelta(t, 0.75, got.HitRate, 0.001)
}

func TestXcodebuildRunner_appendLocalInvocationLog_recordsCIProvider(t *testing.T) {
	runStats := xcodeargs.RunStats{StartTime: time.Now().UTC(), Success: true}

	sut, logger := runnerForLocalLogTest(t, runStats, "bitrise")

	_ = sut.Run(context.Background())

	calls := logger.AppendCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "bitrise", calls[0].Rec.CIProvider)
	assert.False(t, calls[0].Rec.IsLocal())
}

func TestXcodebuildRunner_appendLocalInvocationLog_appendErrorIsNonFatal(t *testing.T) {
	runStats := xcodeargs.RunStats{StartTime: time.Now().UTC(), Success: true, ExitCode: 7}

	sut, logger := runnerForLocalLogTest(t, runStats, "")
	logger.AppendFunc = func(invocations.Record) error { return errors.New("disk full") }

	stats := sut.Run(context.Background())
	assert.Equal(t, 7, stats.ExitCode)
}

func TestXcodebuildRunner_spawnAnalyzerSidecar_spawnsExactlyOnce(t *testing.T) {
	runStats := xcodeargs.RunStats{StartTime: time.Now().UTC(), Success: true}

	sut, _ := runnerForLocalLogTest(t, runStats, "")

	// Fake a derived-data path so the hook does not short-circuit.
	sut.XcodeArgs.(*xcodeargsMocks.XcodeArgsMock).DerivedDataPathFunc = func() string { return t.TempDir() }

	sut.analyzerSidecar = &analyzerSidecarWriterMock{}

	var spawnCalls int
	sut.spawnFn = func(fn func()) {
		spawnCalls++
		// Execute inline so the goroutine's ctx is observable before the
		// test returns; MostRecentActivityLog returns empty for the temp
		// dir and the goroutine exits before it ever reaches the writer.
		fn()
	}

	_ = sut.Run(context.Background())

	assert.Equal(t, 1, spawnCalls, "appendLocalInvocationLog must trigger exactly one spawn")
}

func TestXcodebuildRunner_spawnAnalyzerSidecar_propagatesSchemeAndCommand(t *testing.T) {
	runStats := xcodeargs.RunStats{StartTime: time.Now().UTC(), Success: true}

	sut, _ := runnerForLocalLogTest(t, runStats, "")

	// Fake a derived-data path so the hook does not short-circuit. The mock
	// is pinned to a single dir so sidecar resolution + log seeding below
	// agree on the same filesystem root.
	dd := t.TempDir()
	sut.XcodeArgs.(*xcodeargsMocks.XcodeArgsMock).DerivedDataPathFunc = func() string { return dd }
	sut.XcodeArgs.(*xcodeargsMocks.XcodeArgsMock).SchemeFunc = func() string { return "MyScheme" }
	sut.XcodeArgs.(*xcodeargsMocks.XcodeArgsMock).ShortCommandFunc = func() string { return "build [MyScheme]" }

	sidecar := &analyzerSidecarWriterMock{
		WriteFunc: func(_ context.Context, _ string, _ buildanalyzer.Report) error { return nil },
	}
	sut.analyzerSidecar = sidecar
	sut.spawnFn = func(fn func()) { fn() }

	var (
		gotScheme  string
		gotCommand string
	)
	sut.analyzerRunFn = func(_ context.Context, _ string, opts buildanalyzer.RunOptions) (buildanalyzer.Report, []error, error) {
		gotScheme = opts.Scheme
		gotCommand = opts.Command

		return buildanalyzer.Report{
			Build: buildanalyzer.BuildMeta{Scheme: opts.Scheme, Command: opts.Command},
		}, nil, nil
	}

	// Seed a stub xcactivitylog under the fake derived data so
	// MostRecentActivityLog returns a non-empty path and the sidecar reaches
	// the (fake) analyzer runner. Its contents don't matter — analyzerRunFn
	// is stubbed above.
	logDir := filepath.Join(dd, "Logs", "Build")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "stub.xcactivitylog"), []byte("placeholder"), 0o644))

	_ = sut.Run(context.Background())

	assert.Equal(t, "MyScheme", gotScheme, "scheme must mirror XcodeArgs.Scheme()")
	assert.Equal(t, "build [MyScheme]", gotCommand, "command must mirror inv.Command (XcodeArgs.ShortCommand())")
}

func TestXcodebuildRunner_spawnAnalyzerSidecar_noDerivedData_skipsSpawn(t *testing.T) {
	runStats := xcodeargs.RunStats{StartTime: time.Now().UTC(), Success: true}

	sut, _ := runnerForLocalLogTest(t, runStats, "")
	sut.XcodeArgs.(*xcodeargsMocks.XcodeArgsMock).DerivedDataPathFunc = func() string { return "" }
	sut.NoManagedDD = true

	sut.analyzerSidecar = &analyzerSidecarWriterMock{}

	var spawnCalls int
	sut.spawnFn = func(fn func()) {
		spawnCalls++
		fn()
	}

	_ = sut.Run(context.Background())

	assert.Equal(t, 0, spawnCalls, "no DerivedData → no analyzer work")
}

func runnerWithAnalyzerSidecar(t *testing.T) *XcodebuildRunner {
	t.Helper()

	sut, _ := runnerForLocalLogTest(t, xcodeargs.RunStats{StartTime: time.Now().UTC(), Success: true}, "")
	dd := t.TempDir()
	sut.XcodeArgs.(*xcodeargsMocks.XcodeArgsMock).DerivedDataPathFunc = func() string { return dd }
	sut.analyzerSidecar = &analyzerSidecarWriterMock{}

	return sut
}

// gatedSpawn runs each spawned fn on a real goroutine held until release is called.
func gatedSpawn(t *testing.T, sut *XcodebuildRunner) (release func()) {
	t.Helper()

	gate := make(chan struct{})
	var (
		once sync.Once
		wg   sync.WaitGroup
	)
	release = func() { once.Do(func() { close(gate) }) }

	sut.spawnFn = func(fn func()) {
		wg.Go(func() {
			<-gate
			fn()
		})
	}

	t.Cleanup(func() {
		release()
		wg.Wait()
	})

	return release
}

func TestXcodebuildRunner_Run_waitsForAnalyzerSidecar(t *testing.T) {
	sut := runnerWithAnalyzerSidecar(t)
	release := gatedSpawn(t, sut)

	returned := make(chan struct{})
	go func() {
		_ = sut.Run(context.Background())
		close(returned)
	}()

	select {
	case <-returned:
		t.Fatal("Run returned before the analyzer sidecar finished")
	case <-time.After(300 * time.Millisecond):
	}

	release()

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the analyzer sidecar finished")
	}
}

func TestXcodebuildRunner_Run_analyzerSidecarWaitIsCapped(t *testing.T) {
	sut := runnerWithAnalyzerSidecar(t)
	gatedSpawn(t, sut)
	sut.analyzerWaitCap = 100 * time.Millisecond

	start := time.Now()
	_ = sut.Run(context.Background())
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
	assert.Less(t, elapsed, 5*time.Second)
}

func TestXcodebuildRunner_spawnAnalyzerSidecar_afterInvocationSave(t *testing.T) {
	sut := runnerWithAnalyzerSidecar(t)
	saver := &countingInvocationSaver{}
	sut.invocationAPI = saver

	putsAtSpawn := int32(-1)
	sut.spawnFn = func(fn func()) {
		putsAtSpawn = saver.putCalls.Load()
		fn()
	}

	_ = sut.Run(context.Background())

	assert.Equal(t, int32(1), putsAtSpawn, "sidecar must spawn after the invocation PUT")
}
