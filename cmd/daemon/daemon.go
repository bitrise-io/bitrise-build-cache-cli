// Package daemon exposes CLI subcommands that install the Bitrise Build Cache
// helper processes (xcelerate proxy + ccache storage helper) as long-lived
// OS-supervised services. macOS uses LaunchAgents (ACI-5030); Linux uses
// systemd --user units (ACI-5031).
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
		`macOS uses per-user LaunchAgents under ~/Library/LaunchAgents. ` +
		`Linux uses ` + "`systemctl --user`" + ` units under ~/.config/systemd/user.`,
}

func init() {
	common.RootCmd.AddCommand(daemonCmd)
}
