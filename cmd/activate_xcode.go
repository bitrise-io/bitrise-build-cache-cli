package cmd

import (
	"fmt"

	config "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	xcodeConfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcode"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
)

const (
	activateXcode           = "Activate Bitrise Build Cache for Xcode"
	activateXcodeSuccessful = "✅ Bitrise Build Cache for Xcode activated"

	errFmtCreateXcodeConfig = "failed to create Xcode config: %w"
)

// activateXcodeCmd represents the `xcode` subcommand under `activate`
var activateXcodeCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "xcode",
	Short: "Activate Bitrise Build Cache for Xcode",
	Long: `Activate Bitrise Build Cache for Xcode.
This command will:

- Create a config file at ~/.xcelerate/config.json with the Xcode proxy and wrapper versions.
- Download an executable proxy to enable xcode compilation cache connecting to the Bitrise Build Cache.
- Create an executable wrapper for xcodebuild that will use the proxy to connect to the Bitrise Build Cache.
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof(activateXcode)

		config := config.Xcelerate{
			Xcode: xcodeConfig.Xcode{
				ProxyVersion:           "1.0.0",
				WrapperVersion:         "1.0.0",
				OriginalXcodebuildPath: "/usr/bin/xcodebuild",
				BuildCacheEnabled:      true,
			},
		}

		if err := config.CreateXcodeConfig(); err != nil {
			return fmt.Errorf(errFmtCreateXcodeConfig, err)
		}

		logger.TInfof(activateXcodeSuccessful)

		return nil
	},
}

//nolint:gochecknoglobals
var activateXcodeParams = DefaultActivateXcodeParams()

func init() {
	activateCmd.AddCommand(activateXcodeCmd)
}

type ActivateXcodeParams struct {
}

func DefaultActivateXcodeParams() ActivateXcodeParams {
	return ActivateXcodeParams{}
}
