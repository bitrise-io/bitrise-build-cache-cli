package cmd

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/xcelerate/xcodeargs"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
)

const (
	MsgArgsPassedToXcodebuild = "Arguments passed to xcodebuild: %v"

	ErrExecutingXcode = "Error executing xcodebuild: %v"
)

// rootCmd represents the base command when called without any subcommands
var xcodebuildCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "xcodebuild",
	Short: "TBD",
	Long: `xcodebuild -  Wrapper around xcodebuild to enable Bitrise Build Cache.
TBD`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(xcelerateParams.Debug)

		xcodeArgs := xcodeargs.Default{
			Cmd:          cmd,
			OriginalArgs: xcelerateParams.OrigArgs,
		}

		xcodeRunner := &xcodeargs.DefaultRunner{}

		if err := XcodebuildCmdFn(logger, xcodeRunner, xcodeArgs); err != nil {
			logger.Errorf(ErrExecutingXcode, err)

			return err
		}

		return nil
	},
}

func init() {
	// IMPORTANT: silently skip flags not matching defined ones so we can pass them to xcodebuild
	xcodebuildCmd.FParseErrWhitelist = cobra.FParseErrWhitelist{UnknownFlags: true}
	rootCmd.AddCommand(xcodebuildCmd)
}

func XcodebuildCmdFn(
	logger log.Logger,
	xcodeRunner xcodeargs.Runner,
	xcodeArgs xcodeargs.XcodeArgs,
) error {
	toPass := xcodeArgs.Args()
	logger.TDebugf(MsgArgsPassedToXcodebuild, toPass)

	// Intentionally returning xcode error unwrapped

	//nolint:wrapcheck
	return xcodeRunner.Run(toPass)
}
