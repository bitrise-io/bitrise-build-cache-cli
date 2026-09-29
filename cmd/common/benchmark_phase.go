package common

import (
	"fmt"
	"os/exec"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

var benchmarkPhaseTool string //nolint:gochecknoglobals

// Lite activation cannot ask for a benchmark phase — the query needs a
// workspace, app and workflow, and none of them exist at VM warmup. A build
// tool that has no Go wrapper to resolve it in-process asks through this
// instead, at execution time.
//
// NOT at Gradle configuration time: the phase changes per build, and anything
// read during configuration becomes a configuration-cache input, which would
// invalidate the entry on every build.
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
		envs := utils.AllEnvs()

		metadata := configcommon.NewMetadata(envs, "", func(name string, args ...string) (string, error) {
			out, err := exec.CommandContext(cmd.Context(), name, args...).Output()

			return string(out), err
		}, utils.DefaultOsProxy{}, logger)

		var provider configcommon.BenchmarkPhaseProvider
		if metadata.CIProvider != "" {
			if cred, _, err := live.Default(logger).Resolve(cmd.Context(), envs); err == nil {
				provider = configcommon.NewBenchmarkPhaseClient(consts.BitriseWebsiteBaseURL, cred, logger)
			}
		}

		phase := configcommon.ResolveBenchmarkPhase(benchmarkPhaseTool, metadata, provider, logger)

		// An empty line is a valid answer: no phase is active for this build.
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), phase); err != nil {
			return fmt.Errorf("write benchmark phase: %w", err)
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(benchmarkPhaseCmd)
	benchmarkPhaseCmd.Flags().StringVar(&benchmarkPhaseTool, "tool", configcommon.BuildToolGradle,
		"Build tool to resolve the phase for (gradle, xcode, bazel)")
}
