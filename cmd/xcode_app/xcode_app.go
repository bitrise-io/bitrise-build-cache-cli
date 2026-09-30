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
	Short: "Enable / disable the Bitrise Build Cache override for Xcode.app IDE builds",
	Long: `xcode-app routes Xcode.app (the GUI application) through the Bitrise Build ` +
		`Cache xcelerate-proxy for remote CAS. It writes an override xcconfig under ` +
		`~/.bitrise-xcelerate/ and points XCODE_XCCONFIG_FILE at it via ` +
		"`launchctl setenv`" + `. A LaunchAgent reapplies the env var on every login. ` +
		`This complements ` + "`activate xcode`" + `, which only affects command-line ` +
		"`xcodebuild`" + ` invocations. macOS only.

See docs/xcode-app.md for the end-to-end setup, the SPM caveat, and verification.`,
}

func init() {
	common.RootCmd.AddCommand(xcodeAppCmd)
}
