package get

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/bazelcredhelper"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Bazel invokes `--credential_helper=<path>` as `<path> get`, per the EngFlow
// Credential Helper spec. This hidden root subcommand is the spawn target.

//nolint:gochecknoglobals
var getCmd = &cobra.Command{
	Use:           "get",
	Short:         "Bazel credential helper protocol entry point",
	Hidden:        true,
	SilenceUsage:  true,
	SilenceErrors: true,
	// Bazel spawns the helper N times in parallel with a tight per-invocation
	// timeout — override the root PersistentPreRun (version check, stored-auth
	// hydration) with a no-op. The helper does its own expiry-aware refresh,
	// bounded by Budget, and the root's logging would corrupt stdout anyway.
	PersistentPreRun: func(*cobra.Command, []string) {},
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), bazelcredhelper.Budget)
		defer cancel()

		stderr := cmd.ErrOrStderr()
		envs := utils.AllEnvs()
		resolve := bazelcredhelper.NewResolver(envs, stderr)
		resolveRepoURL := bazelcredhelper.NewRepoURLResolver(envs)
		if err := bazelcredhelper.Run(ctx, cmd.InOrStdin(), cmd.OutOrStdout(), resolve, resolveRepoURL); err != nil {
			_, _ = fmt.Fprintln(stderr, err.Error())

			return fmt.Errorf("run bazel credential helper: %w", err)
		}

		return nil
	},
}

func init() {
	common.RootCmd.AddCommand(getCmd)
}
