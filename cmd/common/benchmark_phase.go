package common

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

var benchmarkPhaseTool string //nolint:gochecknoglobals

// benchmarkPhaseTimeout bounds the whole command: a build tool blocks on it,
// and credential resolution plus a retrying HTTP client have no deadline of
// their own. Overrunning it is answered with "no phase", the same as a failure.
// A var only so tests need not wait it out.
//
//nolint:gochecknoglobals
var benchmarkPhaseTimeout = 10 * time.Second

//nolint:gochecknoglobals
var benchmarkPhaseTools = []string{
	configcommon.BuildToolGradle,
	configcommon.BuildToolXcode,
	configcommon.BuildToolBazel,
}

// Lite activation cannot ask for a benchmark phase — the query needs a
// workspace, app and workflow, and none of them exist at preboot. A build
// tool that has no Go wrapper to resolve it in-process asks through this
// instead, at execution time.
//
// Gradle calls it from configuration time through a ValueSource, which Gradle
// re-runs on every build including configuration-cache hits and which only
// invalidates the entry when the phase it returns actually changes.
//
//nolint:gochecknoglobals
var benchmarkPhaseCmd = &cobra.Command{
	Use:           "benchmark-phase",
	Short:         "Print this build's benchmark phase, resolving it at most once per build",
	Hidden:        true,
	SilenceUsage:  true,
	SilenceErrors: true,
	// Spawned from inside a build; the version nudge and the stored-auth hydrate
	// would add network work to something a build tool waits on.
	PersistentPreRun: func(*cobra.Command, []string) {},
	RunE: func(cmd *cobra.Command, _ []string) error {
		// stderr: stdout is the value the caller reads.
		logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode), log.WithOutput(cmd.ErrOrStderr()))

		// The tool names a file under the state dir, so an unchecked one escapes it.
		if !slices.Contains(benchmarkPhaseTools, benchmarkPhaseTool) {
			return fmt.Errorf("unknown --tool %q, expected one of %v", benchmarkPhaseTool, benchmarkPhaseTools)
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), benchmarkPhaseTimeout)
		defer cancel()

		phase := resolveBenchmarkPhaseWithin(ctx, benchmarkPhaseTool, logger)

		// An empty line is a valid answer: no phase is active for this build.
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), phase); err != nil {
			return fmt.Errorf("write benchmark phase: %w", err)
		}

		return nil
	},
}

// resolveBenchmarkPhaseWithin resolves the phase, or gives up and answers "no
// phase" when ctx expires. The resolution is left running: it holds no lock,
// and a record it writes afterwards spares the build's next invocation.
func resolveBenchmarkPhaseWithin(ctx context.Context, buildTool string, logger log.Logger) string {
	resolved := make(chan string, 1)

	// Read before the goroutine starts: it outlives this function on a timeout,
	// and reading a package var from it would race anything that swaps it.
	resolve := benchmarkPhaseResolveFn

	go func() { resolved <- resolve(ctx, buildTool, logger) }()

	select {
	case phase := <-resolved:
		return phase
	case <-ctx.Done():
		logger.Debugf("Benchmark phase resolution timed out, continuing without one")

		return ""
	}
}

// benchmarkPhaseResolveFn is swappable so the command can be tested without a
// credential store or a network.
//
//nolint:gochecknoglobals
var benchmarkPhaseResolveFn = resolveBenchmarkPhaseNow

func resolveBenchmarkPhaseNow(ctx context.Context, buildTool string, logger log.Logger) string {
	envs := utils.AllEnvs()

	metadata := configcommon.NewMetadata(envs, "", func(name string, args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, name, args...).Output()

		return string(out), err
	}, utils.DefaultOsProxy{}, logger)

	cred, _, credErr := live.Default(logger).Resolve(ctx, envs)

	return configcommon.ResolveBenchmarkPhase(
		buildTool, metadata, benchmarkPhaseProvider(metadata.CIProvider, cred, credErr, logger), logger)
}

// benchmarkPhaseProvider gates the query on both halves of what it needs: a CI
// build to ask about and a workspace to ask for. The xcode wrapper requires the
// same pair, and without the workspace the request could only be answered "".
func benchmarkPhaseProvider(
	ciProvider string,
	cred auth.Credential,
	credErr error,
	logger log.Logger,
) configcommon.BenchmarkPhaseProvider {
	if ciProvider == "" || credErr != nil || cred.WorkspaceID == "" {
		return nil
	}

	return configcommon.NewBenchmarkPhaseClient(consts.BitriseWebsiteBaseURL, cred, logger)
}

func init() {
	RootCmd.AddCommand(benchmarkPhaseCmd)
	benchmarkPhaseCmd.Flags().StringVar(&benchmarkPhaseTool, "tool", configcommon.BuildToolGradle,
		"Build tool to resolve the phase for (gradle, xcode, bazel)")
}
