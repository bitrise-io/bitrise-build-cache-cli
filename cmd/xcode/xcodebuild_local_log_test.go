//go:build unit

package xcode

import (
	"context"
	"errors"
	"testing"
	"time"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
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
		ArgsFunc:         func(_ map[string]string) []string { return nil },
		CommandFunc:      func() string { return "xcodebuild -workspace Foo.xcworkspace" },
		ShortCommandFunc: func() string { return "xcodebuild" },
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
