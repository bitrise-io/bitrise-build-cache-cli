package xcode

import (
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// This command should go under an xcelerate subcommand together with stop-xcode-proxy IMO
var stopXcelerateProxyCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:          "stop-proxy",
	Short:        "TBD",
	Long:         `TBD`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		logger.EnableDebugLog(common.IsDebugLogMode)

		return stopXcelerateProxyCommandFn(utils.DefaultOsProxy{}, logger)
	},
}

func init() {
	xcelerateCommand.AddCommand(stopXcelerateProxyCmd)
}

func stopXcelerateProxyCommandFn(osProxy utils.OsProxy, logger log.Logger) error {
	return xcelerate.StopProxy(logger, osProxy) //nolint:wrapcheck // thin cmd-layer wrapper; caller is cobra
}
