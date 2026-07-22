package xcode_app_casinject

import (
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
)

//nolint:gochecknoglobals
var rootCmd = &cobra.Command{
	Use:   "xcode-app-casinject",
	Short: "Inject RemoteService into Xcode.app .cas-config files (PoC)",
	Long: "PoC: watch DerivedData for .cas-config files written by Xcode.app IDE builds " +
		"and inject RemoteService so the ToolchainCASPlugin opens the xcelerate proxy socket. " +
		"Xcode 26 IDE drops COMPILATION_CACHE_REMOTE_SERVICE_PATH before serializing .cas-config, " +
		"so patching post-write is the only way to engage remote CAS from the IDE build path.",
}

func init() {
	common.RootCmd.AddCommand(rootCmd)
}
