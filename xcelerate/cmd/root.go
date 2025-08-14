package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "xcelerate",
	Short: "Xcelerate - Wrapper around xcodebuild to enable Bitrise Build Cache",
	Long: `Xcelerate -  Wrapper around xcodebuild to enable Bitrise Build Cache.

What does Xcelerate do on a high level?
TBD`,
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&xcelerateParams.Debug, "xcelerate-debug", "", xcelerateParams.Debug, "Enable debug logging mode")
}

func Execute() {
	cmd, _, err := rootCmd.Traverse(os.Args[1:])
	xcelerateParams.OrigArgs = os.Args[1:]

	// default cmd if no cmd is given
	if err == nil && cmd.Use == rootCmd.Use {
		args := append([]string{xcodebuildCmd.Use}, os.Args[1:]...)

		// IMPORTANT: silently skip flags not matching defined ones so we can pass them to xcodebuild
		rootCmd.FParseErrWhitelist = cobra.FParseErrWhitelist{UnknownFlags: true}
		rootCmd.SetArgs(args)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

type XcelerateParams struct {
	Debug    bool
	OrigArgs []string
}

//nolint:gochecknoglobals
var xcelerateParams = XcelerateParams{
	Debug:    false,
	OrigArgs: []string{},
}
