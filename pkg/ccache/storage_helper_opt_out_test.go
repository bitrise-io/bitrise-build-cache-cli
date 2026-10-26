//go:build unit

package ccache

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type optOutDirOsProxy struct {
	utils.DefaultOsProxy
	cwd string
}

func (p *optOutDirOsProxy) Getwd() (string, error) {
	return p.cwd, nil
}

// disableListenerForTest fakes "no helper listening" so CollectAndSendStats
// does not touch a real socket. When the opt-out gate kicks in the listener
// is never consulted; the override exists only so a FAILED gate downgrades
// gracefully into a warn + bail instead of blocking on syscalls.
func disableListenerForTest(t *testing.T) {
	t.Helper()
	prev := isListeningFn
	isListeningFn = func(string) bool { return false }
	t.Cleanup(func() { isListeningFn = prev })
}

type countingLogger struct {
	log.Logger
	skipMessages atomic.Int32
}

func (l *countingLogger) TInfof(format string, _ ...interface{}) {
	if format == "[project-mode] opt-in active, no marker; skipping ccache invocation analytics" {
		l.skipMessages.Add(1)
	}
}

func TestStorageHelper_CollectAndSendStats_LogsSkipLineWhenOptedOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, machineconfig.Write(
		machineconfig.Config{ProjectMode: machineconfig.ModeOptIn},
		utils.DefaultOsProxy{},
		paths.FromHome(home),
	))

	disableListenerForTest(t)

	work := t.TempDir()
	osProxy := &optOutDirOsProxy{cwd: work}
	countedLog := &countingLogger{Logger: log.NewLogger()}

	helper := &StorageHelper{
		config:  ccacheconfig.Config{IPCEndpoint: "/tmp/does-not-matter.sock"},
		params:  StorageHelperParams{},
		osProxy: osProxy,
		logger:  countedLog,

		invocationID: "test-inv",
	}

	helper.CollectAndSendStats(context.Background(), "", "")

	assert.Equal(t, int32(1), countedLog.skipMessages.Load(), "opt-out skip log must fire exactly once")
}

func TestStorageHelper_CollectAndSendStats_RunsWhenMarkerPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, machineconfig.Write(
		machineconfig.Config{ProjectMode: machineconfig.ModeOptIn},
		utils.DefaultOsProxy{},
		paths.FromHome(home),
	))

	disableListenerForTest(t)

	work := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(work, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))

	osProxy := &optOutDirOsProxy{cwd: work}
	countedLog := &countingLogger{Logger: log.NewLogger()}

	helper := &StorageHelper{
		config:  ccacheconfig.Config{IPCEndpoint: "/tmp/does-not-matter.sock"},
		params:  StorageHelperParams{},
		osProxy: osProxy,
		logger:  countedLog,

		invocationID: "test-inv",
	}

	helper.CollectAndSendStats(context.Background(), "", "")

	// Gate NOT hit → skip-log must NOT fire. Downstream will bail at
	// loadSessionInfo (listener faked off) which is fine; we're only asserting
	// gating behavior.
	assert.Equal(t, int32(0), countedLog.skipMessages.Load(), "skip log must not fire when marker present")
}
