// Package daemon exposes CLI subcommands that install the Bitrise Build Cache
// helper processes (xcelerate proxy + ccache storage helper) as long-lived
// OS-supervised services. macOS-only today (ACI-5030); Linux support tracked
// in ACI-5031.
package daemon

import (
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
)

//nolint:gochecknoglobals
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Install and control the Bitrise Build Cache background services",
	Long: `daemon registers the Bitrise Build Cache helper processes (xcelerate proxy and ccache storage helper) ` +
		`as long-lived OS-supervised services so they survive across builds and shells. ` +
		`On macOS the services are managed via per-user LaunchAgents under ~/Library/LaunchAgents. ` +
		`Linux (user systemd) support is tracked in ACI-5031.`,
}

func init() {
	common.RootCmd.AddCommand(daemonCmd)
}
