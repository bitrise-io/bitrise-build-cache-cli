package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	xcelerateconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
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
	probeTimeout        = 500 * time.Millisecond
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

		proxy := readXcelerateInfo(osProxy, decoder)
		ccache := readCcacheInfo(osProxy, decoder)

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

func readXcelerateInfo(osProxy utils.OsProxy, decoder utils.DecoderFactory) serviceInfo {
	cfg, err := xcelerateconfig.ReadConfig(osProxy, decoder)
	switch {
	case err == nil && cfg.ProxySocketPath != "":
		return serviceInfo{Socket: cfg.ProxySocketPath, Status: probeSocket(cfg.ProxySocketPath)}
	case errors.Is(err, fs.ErrNotExist):
		return serviceInfo{Socket: "<not configured — run `bitrise-build-cache activate xcode`>", Status: statusNotConfigured}
	default:
		return serviceInfo{Socket: "<not configured>", Status: statusNotConfigured}
	}
}

func readCcacheInfo(osProxy utils.OsProxy, decoder utils.DecoderFactory) serviceInfo {
	cfg, err := ccacheconfig.ReadConfig(osProxy, decoder)
	switch {
	case err == nil && cfg.IPCEndpoint != "":
		return serviceInfo{Socket: cfg.IPCEndpoint, Status: probeCcacheSocket(cfg.IPCEndpoint)}
	case errors.Is(err, fs.ErrNotExist):
		return serviceInfo{Socket: "<not configured — run `bitrise-build-cache activate c++`>", Status: statusNotConfigured}
	default:
		return serviceInfo{Socket: "<not configured>", Status: statusNotConfigured}
	}
}

func probeSocket(path string) string {
	if _, err := os.Stat(path); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			debugLogger().Debugf("probeSocket: stat %s failed: %v", path, err)
		}

		return statusStopped
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return statusStuck
	}
	_ = conn.Close()

	return statusRunning
}

// probeCcacheSocket uses the ccache protocol's health-check exchange so the
// storage helper sees a clean handshake — a raw dial+close would surface as
// "Capabilities check failed" in the helper's log, and CI asserts on those.
func probeCcacheSocket(path string) string {
	if _, err := os.Stat(path); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			debugLogger().Debugf("probeCcacheSocket: stat %s failed: %v", path, err)
		}

		return statusStopped
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	if err := ccache.SendHealthCheck(ctx, path); err != nil {
		return statusStuck
	}

	return statusRunning
}

func debugLogger() log.Logger {
	return log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
}

func init() {
	infoCmd.Flags().BoolVar(&infoJSON, "json", false, "Emit `{xcelerateProxy, xcelerateProxyStatus, ccacheHelper, ccacheHelperStatus}` as JSON instead of human-readable text.")
	daemonCmd.AddCommand(infoCmd)
}
