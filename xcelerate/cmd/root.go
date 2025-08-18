package cmd

import (
	"os"
	"regexp"

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
	rootCmd.FParseErrWhitelist = cobra.FParseErrWhitelist{UnknownFlags: true}
	rootCmd.PersistentFlags().BoolVarP(&xcelerateParams.Debug, "xcelerate-debug", "", xcelerateParams.Debug, "Enable debug logging mode")
}

func Execute() {
	xcelerateParams.OrigArgs = os.Args[1:]

	filteredArgs := removeSingleDashArgs(xcelerateParams.OrigArgs)
	finalizedArgs := addSubcmdIfNone(filteredArgs)

	rootCmd.SetArgs(finalizedArgs)

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

// Remove single dash args (like `-scheme`) from args as they are definitely for Xcode
func removeSingleDashArgs(args []string) []string {
	filtered := []string{}
	var expr = regexp.MustCompile(`^-\w\w+$`)

	for _, arg := range args {
		if !expr.MatchString(arg) {
			filtered = append(filtered, arg)
		}
	}

	return filtered
}

func addSubcmdIfNone(args []string) []string {
	cmd, _, err := rootCmd.Traverse(os.Args[1:])

	// default cmd if no cmd is given
	if err == nil && cmd.Use == rootCmd.Use {
		updated := append([]string{xcodebuildCmd.Use}, args...)

		// IMPORTANT: silently skip flags not matching defined ones so we can pass them to xcodebuild
		return updated
	}

	return args
}
