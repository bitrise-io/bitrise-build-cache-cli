// Package xcode_app hosts the `bitrise-build-cache xcode-app` cobra
// subcommands.
package xcode_app

import (
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
)

//nolint:gochecknoglobals
var xcodeAppCmd = &cobra.Command{
	Use:   "xcode-app",
	Short: "Wire an Xcode project or workspace to the Bitrise Build Cache override xcconfig",
	Long: `xcode-app wires an .xcodeproj (or every .xcodeproj in an .xcworkspace) to the ` +
		`override xcconfig written by ` + "`activate xcode`" + `. With link in place, Xcode.app's ` +
		`IDE builds (⌘B / ▶) route through the Bitrise Build Cache xcelerate-proxy for remote ` +
		`CAS; it complements ` + "`activate xcode`" + `, which only affects command-line ` +
		"`xcodebuild`" + ` invocations. macOS only.

See docs/xcode-app.md for the end-to-end setup, the SPM caveat, and verification.`,
}

func init() {
	common.RootCmd.AddCommand(xcodeAppCmd)
}
