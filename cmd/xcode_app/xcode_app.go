package xcode_app

import (
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
)

//nolint:gochecknoglobals
var xcodeAppCmd = &cobra.Command{
	Use:   "xcode-app",
	Short: "Enable / disable the Bitrise Build Cache override for Xcode.app GUI builds",
	Long: `xcode-app configures Xcode.app (the GUI application) to use the Bitrise Build Cache by writing ` +
		`an override xcconfig under ~/.bitrise-xcelerate/ and pointing XCODE_XCCONFIG_FILE at it via launchctl. ` +
		`This complements ` + "`xcodebuild`" + ` activation, which only affects command-line builds. ` +
		`macOS only.`,
}

func init() {
	common.RootCmd.AddCommand(xcodeAppCmd)
}
