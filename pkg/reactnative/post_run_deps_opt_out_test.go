//go:build unit

package reactnative

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/analytics/multiplatform"
	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type rnOptOutDirOsProxy struct {
	utils.DefaultOsProxy
	cwd string
}

func (p *rnOptOutDirOsProxy) Getwd() (string, error) {
	return p.cwd, nil
}

type rnCountingLogger struct {
	log.Logger
	skipMessages atomic.Int32
}

func (l *rnCountingLogger) TInfof(format string, _ ...interface{}) {
	if format == "[project-mode] opt-in active, no marker; skipping React Native invocation analytics" {
		l.skipMessages.Add(1)
	}
}

func TestPostRunDeps_run_optedOut_skipsSendAndLocalLog(t *testing.T) {
	// Opt-in + no marker → run must skip sendInvocation and appendLocalInvocationLog.
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, machineconfig.Write(
		machineconfig.Config{ProjectMode: machineconfig.ModeOptIn},
		utils.DefaultOsProxy{},
		paths.FromHome(home),
	))

	// Keep ccache off for the test to not try to connect to a storage helper.
	// activate-react-native in baseline mode intentionally skips the ccache
	// path (post_run_deps.go:111) — we hitch on that behavior here.
	t.Setenv("BITRISE_BUILD_CACHE_BENCHMARK_PHASE_GRADLE", "baseline")

	work := t.TempDir()
	osProxy := &rnOptOutDirOsProxy{cwd: work}
	countedLog := &rnCountingLogger{Logger: log.NewLogger()}

	localLogger := &localInvocationLoggerMock{
		AppendFunc: func(invocations.Record) error { return nil },
	}
	client := &invocationClientMock{
		PutInvocationFunc: func(multiplatform.Invocation) error { return nil },
	}

	deps := &postRunDeps{
		logger:      countedLog,
		localLogger: localLogger,
		osProxy:     osProxy,
		client:      client,
	}

	_ = deps.run(context.Background(), "test-inv", []string{"yarn", "build"}, 500*time.Millisecond, nil)

	assert.Equal(t, int32(1), countedLog.skipMessages.Load(), "RN opt-out skip log must fire exactly once")
	assert.Empty(t, client.PutInvocationCalls(), "sendInvocation must NOT be called when opted out")
	assert.Empty(t, localLogger.AppendCalls(), "appendLocalInvocationLog must NOT be called when opted out")
}

func TestPostRunDeps_run_notOptedOut_logsNoSkipLine(t *testing.T) {
	// Opt-in + marker present → gate must NOT fire. Skip-log stays silent AND
	// the stub client receives the invocation (proves the stub is reachable;
	// without this the opted-out assertion could pass trivially).
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, machineconfig.Write(
		machineconfig.Config{ProjectMode: machineconfig.ModeOptIn},
		utils.DefaultOsProxy{},
		paths.FromHome(home),
	))

	t.Setenv("BITRISE_BUILD_CACHE_BENCHMARK_PHASE_GRADLE", "baseline")

	work := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(work, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))
	osProxy := &rnOptOutDirOsProxy{cwd: work}

	countedLog := &rnCountingLogger{Logger: log.NewLogger()}

	localLogger := &localInvocationLoggerMock{
		AppendFunc: func(invocations.Record) error { return nil },
	}
	client := &invocationClientMock{
		PutInvocationFunc: func(multiplatform.Invocation) error { return nil },
	}

	deps := &postRunDeps{
		logger:      countedLog,
		localLogger: localLogger,
		osProxy:     osProxy,
		client:      client,
	}

	_ = deps.run(context.Background(), "test-inv", []string{"yarn", "build"}, 500*time.Millisecond, nil)

	assert.Equal(t, int32(0), countedLog.skipMessages.Load(), "RN skip log must NOT fire when marker present")
	assert.Len(t, client.PutInvocationCalls(), 1, "sendInvocation must fire exactly once in the happy path")
	assert.Len(t, localLogger.AppendCalls(), 1, "appendLocalInvocationLog must fire exactly once in the happy path")
}
