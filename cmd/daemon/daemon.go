package daemon

import (
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
)

func resolveBackendAndPaths() (daemonpkg.Backend, daemonpkg.Paths, error) {
	backend, err := daemonpkg.DefaultBackend()
	if err != nil {
		return nil, daemonpkg.Paths{}, err //nolint:wrapcheck // sentinel
	}

	paths, err := daemonpkg.NewPaths()
	if err != nil {
		return nil, daemonpkg.Paths{}, err //nolint:wrapcheck // already context-rich
	}

	return backend, paths, nil
}

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
