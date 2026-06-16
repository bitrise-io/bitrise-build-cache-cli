package daemon

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/spf13/cobra"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	xcelerateconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

//nolint:gochecknoglobals
var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Print the daemon socket paths (for IDE / external-tool configuration)",
	Long: `info prints the unix socket paths exposed by the supervised services. ` +
		`Use these when wiring up an IDE (e.g. Xcode.app's COMPILATION_CACHE_REMOTE_SERVICE_PATH) ` +
		`or any external consumer that talks to the daemon directly. ` +
		`Sockets that aren't configured yet print "<not configured>".`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		osProxy := utils.DefaultOsProxy{}
		decoder := utils.DefaultDecoderFactory{}

		out := cmd.OutOrStdout()

		proxyPath := readXcelerateSocketPath(osProxy, decoder)
		ccachePath := readCcacheSocketPath(osProxy, decoder)

		fmt.Fprintf(out, "xcelerate-proxy: %s\n", proxyPath)
		fmt.Fprintf(out, "ccache-helper:   %s\n", ccachePath)

		return nil
	},
}

func readXcelerateSocketPath(osProxy utils.OsProxy, decoder utils.DecoderFactory) string {
	cfg, err := xcelerateconfig.ReadConfig(osProxy, decoder)
	switch {
	case err == nil && cfg.ProxySocketPath != "":
		return cfg.ProxySocketPath
	case errors.Is(err, fs.ErrNotExist):
		return "<not configured — run `bitrise-build-cache activate xcode`>"
	default:
		return "<not configured>"
	}
}

func readCcacheSocketPath(osProxy utils.OsProxy, decoder utils.DecoderFactory) string {
	cfg, err := ccacheconfig.ReadConfig(osProxy, decoder)
	switch {
	case err == nil && cfg.IPCEndpoint != "":
		return cfg.IPCEndpoint
	case errors.Is(err, fs.ErrNotExist):
		return "<not configured — run `bitrise-build-cache activate c++`>"
	default:
		return "<not configured>"
	}
}

func init() {
	daemonCmd.AddCommand(infoCmd)
}
