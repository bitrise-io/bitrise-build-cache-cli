//go:build unit

package common

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

// runBenchmarkPhaseCmd runs the command with the resolution stubbed out, so the
// test exercises the contract the build tool sees, not a credential store.
func runBenchmarkPhaseCmd(t *testing.T, args []string, resolve func(string) string) (string, string, error) {
	t.Helper()

	return runBenchmarkPhaseCmdWith(t, args, func(_ context.Context, buildTool string, logger log.Logger) string {
		logger.Infof("resolving %s", buildTool)

		return resolve(buildTool)
	})
}

func runBenchmarkPhaseCmdWith(
	t *testing.T,
	args []string,
	resolve func(context.Context, string, log.Logger) string,
) (string, string, error) {
	t.Helper()

	origFn, origTool := benchmarkPhaseResolveFn, benchmarkPhaseTool
	t.Cleanup(func() {
		benchmarkPhaseResolveFn, benchmarkPhaseTool = origFn, origTool
		benchmarkPhaseCmd.SetOut(nil)
		benchmarkPhaseCmd.SetErr(nil)
		RootCmd.SetOut(nil)
		RootCmd.SetErr(nil)
		RootCmd.SetArgs(nil)
	})

	benchmarkPhaseResolveFn = resolve

	var stdout, stderr bytes.Buffer
	benchmarkPhaseCmd.SetOut(&stdout)
	benchmarkPhaseCmd.SetErr(&stderr)

	// cobra keeps the context a command was first executed with, so a second run
	// in the same process would inherit the previous test's cancelled one.
	benchmarkPhaseCmd.SetContext(t.Context())

	// Through the root: cobra routes Execute on a subcommand to its root anyway.
	RootCmd.SetArgs(append([]string{"benchmark-phase"}, args...))
	RootCmd.SetOut(&stdout)
	RootCmd.SetErr(&stderr)

	err := RootCmd.ExecuteContext(t.Context())

	return stdout.String(), stderr.String(), err
}

// The caller reads stdout as the phase, so nothing else may reach it — the
// logger is wired to stderr for exactly this reason.
func TestBenchmarkPhaseCmd_StdoutCarriesOnlyThePhase(t *testing.T) {
	stdout, stderr, err := runBenchmarkPhaseCmd(t, []string{"--tool", "xcode"}, func(tool string) string {
		assert.Equal(t, configcommon.BuildToolXcode, tool)

		return configcommon.BenchmarkPhaseWarmup
	})

	require.NoError(t, err)
	assert.Equal(t, "warmup\n", stdout)
	assert.NotContains(t, stdout, "resolving")
	assert.Contains(t, stderr, "resolving")
}

// No phase is the common answer and is not a failure: a non-zero exit would
// make the build tool treat a perfectly normal build as broken.
func TestBenchmarkPhaseCmd_NoPhaseIsAnEmptyLineAndASuccess(t *testing.T) {
	stdout, _, err := runBenchmarkPhaseCmd(t, nil, func(string) string { return "" })

	require.NoError(t, err)
	assert.Equal(t, "\n", stdout)
}

// The tool names a file under the state dir; an unvalidated one escapes it.
func TestBenchmarkPhaseCmd_RejectsAnUnknownTool(t *testing.T) {
	called := false
	stdout, _, err := runBenchmarkPhaseCmd(t, []string{"--tool", "../../x"}, func(string) string {
		called = true

		return ""
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "../../x")
	assert.Empty(t, stdout)
	assert.False(t, called, "a rejected tool must not reach the resolver")
}

func TestBenchmarkPhaseCmd_AcceptsEveryKnownTool(t *testing.T) {
	for _, tool := range []string{configcommon.BuildToolGradle, configcommon.BuildToolXcode, configcommon.BuildToolBazel} {
		t.Run(tool, func(t *testing.T) {
			_, _, err := runBenchmarkPhaseCmd(t, []string{"--tool", tool}, func(string) string { return "" })
			require.NoError(t, err)
		})
	}
}

// A build tool blocks on this command, and neither credential resolution nor
// the retrying HTTP client has a deadline of its own.
func TestResolveBenchmarkPhaseWithin_GivesUpWhenTheDeadlinePasses(t *testing.T) {
	origFn := benchmarkPhaseResolveFn
	t.Cleanup(func() { benchmarkPhaseResolveFn = origFn })

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	benchmarkPhaseResolveFn = func(context.Context, string, log.Logger) string {
		<-release

		return configcommon.BenchmarkPhaseBaseline
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	assert.Empty(t, resolveBenchmarkPhaseWithin(ctx, configcommon.BuildToolGradle, log.NewLogger()))
}

// Both halves are needed: a build to ask about and a workspace to ask for. The
// xcode wrapper gates on the same pair, and a query without either can only be
// answered "".
func TestBenchmarkPhaseProvider_QueriesOnlyWithABuildAndAWorkspace(t *testing.T) {
	cred := auth.Credential{WorkspaceID: "ws-1", Token: "t"}

	assert.Nil(t, benchmarkPhaseProvider("", cred, nil, log.NewLogger()), "off CI there is no build to ask about")
	assert.Nil(t, benchmarkPhaseProvider(configcommon.CIProviderBitrise, auth.Credential{}, nil, log.NewLogger()),
		"without a workspace the request cannot be made")
	assert.Nil(t, benchmarkPhaseProvider(configcommon.CIProviderBitrise, cred, errors.New("no credential"), log.NewLogger()))
	assert.NotNil(t, benchmarkPhaseProvider(configcommon.CIProviderBitrise, cred, nil, log.NewLogger()))
}

// The deadline has to be on the command, not only inside the helper: the build
// tool is blocked on this process, so an unreachable API must not hold it.
func TestBenchmarkPhaseCmd_GivesUpWhenResolutionOverrunsTheDeadline(t *testing.T) {
	origTimeout := benchmarkPhaseTimeout
	benchmarkPhaseTimeout = 20 * time.Millisecond
	t.Cleanup(func() { benchmarkPhaseTimeout = origTimeout })

	// Writes nothing and touches no test buffer: it outlives the command.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	stdout, _, err := runBenchmarkPhaseCmdWith(t, nil, func(context.Context, string, log.Logger) string {
		<-release

		return configcommon.BenchmarkPhaseBaseline
	})

	require.NoError(t, err)
	assert.Equal(t, "\n", stdout)
}
