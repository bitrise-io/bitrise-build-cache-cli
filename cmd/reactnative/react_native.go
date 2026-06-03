package reactnative

import (
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
)

//nolint:gochecknoglobals
var reactNativeCmd = &cobra.Command{
	Use:   "react-native",
	Short: "Commands for React Native build cache",
	Long:  `Commands for configuring and running build cache for React Native projects.`,
}

func init() {
	common.RootCmd.AddCommand(reactNativeCmd)
}
