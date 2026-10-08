package xcode

import (
	"os"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

//nolint:gochecknoglobals
var flushSessionCmd = &cobra.Command{
	Use:          "flush-session",
	Short:        "Force the running proxy to enrich any pending orphan invocations and print their Visit URLs",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		logger.EnableDebugLog(common.IsDebugLogMode)

		return xcelerate.FlushSession(cmd.Context(), logger, utils.DefaultOsProxy{}, os.Stdout) //nolint:wrapcheck
	},
}

func init() {
	xcelerateCommand.AddCommand(flushSessionCmd)
}
