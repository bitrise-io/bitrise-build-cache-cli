package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/spf13/cobra"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	xcelerateconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

//nolint:gochecknoglobals
var infoJSON bool

type serviceInfo struct {
	Socket string `json:"socket"`
	Status string `json:"status"`
}

const (
	statusRunning       = "running"
	statusStopped       = "stopped"
	statusStuck         = "stuck (socket present, not responding — run `bitrise-build-cache doctor --fix` or `bitrise-build-cache daemon restart`)"
	statusNotConfigured = "not configured"
)

//nolint:gochecknoglobals
var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Report the daemon service sockets + running status",
	Long: `info prints the unix socket paths exposed by the supervised services and probes each ` +
		`socket to report whether the service is currently accepting connections. Use the socket ` +
		`paths when wiring up an IDE (e.g. Xcode.app's COMPILATION_CACHE_REMOTE_SERVICE_PATH); use ` +
		`the status to tell whether the daemon is actually up.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		osProxy := utils.DefaultOsProxy{}
		decoder := utils.DefaultDecoderFactory{}

		out := cmd.OutOrStdout()

		proxy := readXcelerateInfo(cmd.Context(), osProxy, decoder)
		ccache := readCcacheInfo(cmd.Context(), osProxy, decoder)

		if infoJSON {
			payload := struct {
				XcelerateProxy       string `json:"xcelerateProxy"`
				XcelerateProxyStatus string `json:"xcelerateProxyStatus"`
				CcacheHelper         string `json:"ccacheHelper"`
				CcacheHelperStatus   string `json:"ccacheHelperStatus"`
			}{
				XcelerateProxy:       proxy.Socket,
				XcelerateProxyStatus: proxy.Status,
				CcacheHelper:         ccache.Socket,
				CcacheHelperStatus:   ccache.Status,
			}

			if err := json.NewEncoder(out).Encode(payload); err != nil {
				return fmt.Errorf("encode info json: %w", err)
			}

			return nil
		}

		fmt.Fprintf(out, "xcelerate-proxy: %s\n", proxy.Socket)
		fmt.Fprintf(out, "ccache-helper:   %s\n", ccache.Socket)
		fmt.Fprintln(out)
		fmt.Fprintf(out, "xcelerate-proxy status: %s\n", proxy.Status)
		fmt.Fprintf(out, "ccache-helper status:   %s\n", ccache.Status)

		return nil
	},
}

func readXcelerateInfo(ctx context.Context, osProxy utils.OsProxy, decoder utils.DecoderFactory) serviceInfo {
	cfg, err := xcelerateconfig.ReadConfig(osProxy, decoder, utils.AllEnvs())
	switch {
	case err == nil && cfg.ProxySocketPath != "":
		return serviceInfo{Socket: cfg.ProxySocketPath, Status: probeStatus(daemonpkg.ProbeSocket(ctx, cfg.ProxySocketPath))}
	case errors.Is(err, fs.ErrNotExist):
		return serviceInfo{Socket: "<not configured — run `bitrise-build-cache activate xcode`>", Status: statusNotConfigured}
	default:
		return serviceInfo{Socket: "<not configured>", Status: statusNotConfigured}
	}
}

func readCcacheInfo(ctx context.Context, osProxy utils.OsProxy, decoder utils.DecoderFactory) serviceInfo {
	cfg, err := ccacheconfig.ReadConfig(osProxy, decoder, utils.AllEnvs())
	switch {
	case err == nil && cfg.IPCEndpoint != "":
		return serviceInfo{Socket: cfg.IPCEndpoint, Status: probeStatus(daemonpkg.ProbeCcacheSocket(ctx, cfg.IPCEndpoint))}
	case errors.Is(err, fs.ErrNotExist):
		return serviceInfo{Socket: "<not configured — run `bitrise-build-cache activate c++`>", Status: statusNotConfigured}
	default:
		return serviceInfo{Socket: "<not configured>", Status: statusNotConfigured}
	}
}

func probeStatus(p daemonpkg.SocketProbe) string {
	switch p {
	case daemonpkg.ProbeRunning:
		return statusRunning
	case daemonpkg.ProbeStuck:
		return statusStuck
	case daemonpkg.ProbeStopped:
		return statusStopped
	default:
		return statusStopped
	}
}

func init() {
	infoCmd.Flags().BoolVar(&infoJSON, "json", false, "Emit `{xcelerateProxy, xcelerateProxyStatus, ccacheHelper, ccacheHelperStatus}` as JSON instead of human-readable text.")
	daemonCmd.AddCommand(infoCmd)
}
