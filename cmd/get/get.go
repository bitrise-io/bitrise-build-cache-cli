package get

import (
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
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := bazelcredhelper.Run(cmd.InOrStdin(), cmd.OutOrStdout(), utils.AllEnvs()); err != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err.Error())

			return fmt.Errorf("run bazel credential helper: %w", err)
		}

		return nil
	},
}

func init() {
	common.RootCmd.AddCommand(getCmd)
}
