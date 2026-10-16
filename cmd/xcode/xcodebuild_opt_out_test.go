//go:build unit

package xcode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/analytics/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs/mocks"
)

type countingRelationSender struct {
	calls int
}

func (s *countingRelationSender) PutInvocationRelation(_ multiplatform.InvocationRelation) error {
	s.calls++

	return nil
}

type countingLocalLogger struct {
	calls int
}

func (l *countingLocalLogger) Append(_ invocations.Record) error {
	l.calls++

	return nil
}

// projectDirOsProxy overrides Getwd on top of DefaultOsProxy so marker
// lookup walks from a test-controlled directory.
type projectDirOsProxy struct {
	utils.DefaultOsProxy
	cwd string
}

func (p *projectDirOsProxy) Getwd() (string, error) {
	return p.cwd, nil
}

func writeProjectMarker(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))
}

func optOutXcodeTestLogger() *utilsMocks.Logger {
	logMock := &utilsMocks.Logger{}
	for _, name := range []string{"TDebugf", "TInfof", "TDonef", "TErrorf", "Errorf", "Warnf", "Debugf", "Infof"} {
		logMock.On(name, mock.Anything).Return()
		logMock.On(name, mock.Anything, mock.Anything).Return()
		logMock.On(name, mock.Anything, mock.Anything, mock.Anything).Return()
		logMock.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	}

	return logMock
}

func TestXcodebuildRunner_optedOut_skipsAnalyticsAndLocalLog(t *testing.T) {
	// Opt-in mode + no marker up the tree → analytics suppressed.
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, machineconfig.Write(
		machineconfig.Config{ProjectMode: machineconfig.ModeOptIn},
		utils.DefaultOsProxy{},
		paths.FromHome(home),
	))

	work := t.TempDir()
	osProxy := &projectDirOsProxy{cwd: work}

	xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
		HasBuildActionFunc: func() bool { return true },
		ArgsFunc:           func(_ map[string]string) []string { return nil },
		CommandFunc:        func() string { return "xcodebuild -workspace Foo.xcworkspace" },
		ShortCommandFunc:   func() string { return "xcodebuild" },
	}
	runStats := xcodeargs.RunStats{StartTime: time.Now().UTC(), Success: true}

	invocationSaver := &countingInvocationSaver{}
	relationSender := &countingRelationSender{}
	localLogger := &countingLocalLogger{}

	runner := &XcodebuildRunner{
		Config:        xcelerate.Config{},
		Metadata:      common.CacheConfigMetadata{CIProvider: "", CLIVersion: "v2.8.6"},
		InvocationID:  "test-inv",
		Logger:        optOutXcodeTestLogger(),
		CacheLogger:   optOutXcodeTestLogger(),
		XcodeRunner:   &fakeXcodeRunner{stats: runStats},
		XcodeArgs:     &xcodeArgProvider,
		OsProxy:       osProxy,
		invocationAPI: invocationSaver,
		relationAPI:   relationSender,
		localLogger:   localLogger,
	}

	_ = runner.Run(context.Background())

	assert.Equal(t, int32(0), invocationSaver.putCalls.Load(), "PutInvocation must NOT be called when opted out")
	assert.Equal(t, 0, relationSender.calls, "PutInvocationRelation must NOT be called when opted out")
	assert.Equal(t, 0, localLogger.calls, "Local invocation log must NOT be appended when opted out")
}

func TestXcodebuildRunner_notOptedOut_emitsAnalytics(t *testing.T) {
	// Opt-in mode WITH a marker → analytics fires.
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, machineconfig.Write(
		machineconfig.Config{ProjectMode: machineconfig.ModeOptIn},
		utils.DefaultOsProxy{},
		paths.FromHome(home),
	))

	work := t.TempDir()
	writeProjectMarker(t, work)
	osProxy := &projectDirOsProxy{cwd: work}

	xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
		HasBuildActionFunc: func() bool { return true },
		ArgsFunc:           func(_ map[string]string) []string { return nil },
		CommandFunc:        func() string { return "xcodebuild -workspace Foo.xcworkspace" },
		ShortCommandFunc:   func() string { return "xcodebuild" },
	}
	runStats := xcodeargs.RunStats{StartTime: time.Now().UTC(), Success: true}

	invocationSaver := &countingInvocationSaver{}
	localLogger := &countingLocalLogger{}

	runner := &XcodebuildRunner{
		Config:        xcelerate.Config{},
		Metadata:      common.CacheConfigMetadata{CIProvider: "", CLIVersion: "v2.8.6"},
		InvocationID:  "test-inv",
		Logger:        optOutXcodeTestLogger(),
		CacheLogger:   optOutXcodeTestLogger(),
		XcodeRunner:   &fakeXcodeRunner{stats: runStats},
		XcodeArgs:     &xcodeArgProvider,
		OsProxy:       osProxy,
		invocationAPI: invocationSaver,
		relationAPI:   &countingRelationSender{},
		localLogger:   localLogger,
	}

	_ = runner.Run(context.Background())

	assert.Equal(t, int32(1), invocationSaver.putCalls.Load(), "PutInvocation must fire when marker present")
	assert.Equal(t, 1, localLogger.calls, "Local invocation log must be appended when marker present")
}
