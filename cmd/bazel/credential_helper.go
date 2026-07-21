package bazel

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/bazelcredhelper"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

//nolint:gochecknoglobals
var credentialHelperCmd = &cobra.Command{
	Use:           "credential-helper",
	Short:         "Bazel credential helper: emits an authorization Bearer header for build cache RPCs",
	Long:          "Implements the Bazel `--credential_helper` JSON protocol so `~/.bazelrc` can point at this binary instead of embedding the auth token. Reads one JSON request from stdin, resolves the token via the same precedence chain as the rest of the CLI (env vars → OS keychain → multiplatform config; env vars are hydrated by the root PersistentPreRun), and writes a JSON response with an `authorization: Bearer <token>` header. Exit 0 on success.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := bazelcredhelper.Run(cmd.InOrStdin(), cmd.OutOrStdout(), utils.AllEnvs()); err != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err.Error())

			return fmt.Errorf("run bazel credential helper: %w", err)
		}

		return nil
	},
}

func init() {
	bazelCmd.AddCommand(credentialHelperCmd)
}
