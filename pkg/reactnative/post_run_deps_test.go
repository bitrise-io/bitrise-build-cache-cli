//go:build unit

package reactnative

import (
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/common/childstats"
)

func TestRNInvocationDetailsURL_WithWorkspace(t *testing.T) {
	got := rnInvocationDetailsURL("ws-slug", "inv-id")
	assert.Equal(t,
		"https://app.bitrise.io/build-cache/ws-slug/invocations/react-native/inv-id",
		got,
	)
}

// depsForLocalLogTest builds a postRunDeps configured with a mock local
// logger and a silent test logger — enough surface to exercise
// appendLocalInvocationLog without touching the analytics client or ccache.
func depsForLocalLogTest(t *testing.T) (*postRunDeps, *localInvocationLoggerMock) {
	t.Helper()

	logMock := &utilsMocks.Logger{}
	for _, name := range []string{"TDebugf", "TInfof", "TDonef", "TErrorf", "TWarnf", "Errorf", "Warnf", "Debugf", "Infof"} {
		logMock.On(name, mock.Anything).Return()
		logMock.On(name, mock.Anything, mock.Anything).Return()
		logMock.On(name, mock.Anything, mock.Anything, mock.Anything).Return()
		logMock.On(name, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	}

	logger := &localInvocationLoggerMock{
		AppendFunc: func(invocations.Record) error { return nil },
	}

	return &postRunDeps{
		logger:      logMock,
		localLogger: logger,
	}, logger
}

func TestPostRunDeps_appendLocalInvocationLog_writesRecord(t *testing.T) {
	deps, logger := depsForLocalLogTest(t)

	metadata := common.CacheConfigMetadata{
		CLIVersion:   "v3.0.1",
		CIProvider:   "",
		HostMetadata: common.HostMetadata{Username: "dev-user"},
	}
	summary := childstats.Summary{}

	before := time.Now().UTC()
	deps.appendLocalInvocationLog("inv-42", "yarn build", metadata, summary, 750*time.Millisecond, nil)
	after := time.Now().UTC()

	calls := logger.AppendCalls()
	require.Len(t, calls, 1)

	rec := calls[0].Rec
	assert.Equal(t, "inv-42", rec.InvocationID)
	assert.Equal(t, "yarn build", rec.Command)
	assert.Equal(t, invocations.ToolRN, rec.Tool)
	assert.Empty(t, rec.ToolVersion)
	assert.Equal(t, "v3.0.1", rec.CLIVersion)
	assert.Equal(t, 0, rec.ExitCode)
	assert.Empty(t, rec.CIProvider)
	assert.True(t, rec.IsLocal())
	assert.Equal(t, "dev-user", rec.Username)
	assert.InDelta(t, 0.0, rec.HitRate, 0.001)

	// FinishedAt is time.Now() at emit — bounded by the surrounding window.
	assert.False(t, rec.FinishedAt.Before(before))
	assert.False(t, rec.FinishedAt.After(after))
	// StartedAt = FinishedAt - duration
	assert.Equal(t, rec.FinishedAt.Add(-750*time.Millisecond), rec.StartedAt)
}

func TestPostRunDeps_appendLocalInvocationLog_exitErrorPropagatesCode(t *testing.T) {
	deps, logger := depsForLocalLogTest(t)

	execCmd := exec.Command("bash", "-c", "exit 137")
	execErr := execCmd.Run()

	metadata := common.CacheConfigMetadata{CLIVersion: "v3.0.1"}
	deps.appendLocalInvocationLog("inv-x", "yarn build", metadata, childstats.Summary{}, time.Second, execErr)

	calls := logger.AppendCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, 137, calls[0].Rec.ExitCode)
}

func TestPostRunDeps_appendLocalInvocationLog_genericErrorMapsToOne(t *testing.T) {
	deps, logger := depsForLocalLogTest(t)

	metadata := common.CacheConfigMetadata{CLIVersion: "v3.0.1"}
	deps.appendLocalInvocationLog("inv-y", "yarn build", metadata, childstats.Summary{}, time.Second, errors.New("launch failed"))

	calls := logger.AppendCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, 1, calls[0].Rec.ExitCode)
}

func TestPostRunDeps_appendLocalInvocationLog_appendErrorIsNonFatal(t *testing.T) {
	deps, logger := depsForLocalLogTest(t)
	logger.AppendFunc = func(invocations.Record) error { return errors.New("disk full") }

	// Must not panic or crash — warn-only failure surface.
	deps.appendLocalInvocationLog("inv-z", "yarn build", common.CacheConfigMetadata{}, childstats.Summary{}, time.Second, nil)

	require.Len(t, logger.AppendCalls(), 1)
}

func TestPostRunDeps_appendLocalInvocationLog_ciProviderPropagates(t *testing.T) {
	deps, logger := depsForLocalLogTest(t)

	metadata := common.CacheConfigMetadata{CIProvider: "bitrise", CLIVersion: "v3.0.1"}
	deps.appendLocalInvocationLog("inv-ci", "yarn build", metadata, childstats.Summary{}, time.Second, nil)

	calls := logger.AppendCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "bitrise", calls[0].Rec.CIProvider)
	assert.False(t, calls[0].Rec.IsLocal())
}

func TestPostRunDeps_appendLocalInvocationLog_hitRateFromSummary(t *testing.T) {
	deps, logger := depsForLocalLogTest(t)

	summary := childstats.Summary{ChildCount: 3, MeanHitRate: 0.62}
	deps.appendLocalInvocationLog("inv-hr", "yarn build", common.CacheConfigMetadata{}, summary, time.Second, nil)

	calls := logger.AppendCalls()
	require.Len(t, calls, 1)
	assert.InDelta(t, 0.62, calls[0].Rec.HitRate, 0.001)
}

func TestPostRunDeps_appendLocalInvocationLog_usernameFromEnvChain(t *testing.T) {
	// Explicitly exercise the real ResolveUsername chain: setting
	// BITRISE_BUILD_CACHE_USERNAME must flow through common.NewMetadata → the
	// Record.Username field, mirroring how the wrapper runs in production.
	t.Setenv(common.EnvUsername, "env-set-user")

	deps, logger := depsForLocalLogTest(t)

	envs := utils.AllEnvs()
	metadata := common.NewMetadata(envs, func(string, ...string) (string, error) { return "", nil }, log.NewLogger())

	deps.appendLocalInvocationLog("inv-un", "yarn build", metadata, childstats.Summary{}, time.Second, nil)

	calls := logger.AppendCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "env-set-user", calls[0].Rec.Username)
}
