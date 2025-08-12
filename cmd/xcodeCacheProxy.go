package cmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/grpcutil"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode_cache_proxy"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/kv_storage"
)

// activateGradleCmd represents the `gradle` subcommand under `activate`
var xcodeCacheProxyCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:          "xcode-proxy",
	Short:        "Xcode Cache Proxy",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Xcode Cache Proxy")

		socketPath := os.Getenv("SOCKET_PATH")
		if socketPath == "" {
			socketPath = filepath.Join(os.TempDir(), "xcode-cache-proxy.sock")
		}

		logger.TInfof("socketPath: %s", socketPath)

		targetAddr := os.Getenv("TARGET_ADDR")
		if targetAddr == "" {
			return errors.New("no TARGET_ADDR specified")
		}

		invocationID := os.Getenv("INVOCATION_ID")
		token := os.Getenv("REMOTE_CACHE_TOKEN")
		appSlug := os.Getenv("BITRISE_APP_SLUG")
		orgID := os.Getenv("BITRISE_ORG_ID")
		buildSlug := os.Getenv("BITRISE_BUILD_SLUG")
		stepExecutionID := os.Getenv("BITRISE_STEP_EXECUTION_ID")

		if err := os.RemoveAll(socketPath); err != nil {
			return fmt.Errorf("failed to remove existing socket: %w", err)
		}

		listener, err := (&net.ListenConfig{}).Listen(cmd.Context(), "unix", socketPath)
		if err != nil {
			return fmt.Errorf("failed to listen on unix socket: %w", err)
		}

		client, err := grpcutil.NewGRPCClient(targetAddr)
		if err != nil {
			return fmt.Errorf("failed to create proxy: %w", err)
		}

		grpcServer := xcode_cache_proxy.NewProxy(
			kv_storage.NewKVStorageClient(client),
			remoteexecution.NewCapabilitiesClient(client),
			token,
			appSlug,
			orgID,
			invocationID,
			buildSlug,
			stepExecutionID,
			logger,
		)

		return grpcServer.Serve(listener)
	},
}

func init() {
	rootCmd.AddCommand(xcodeCacheProxyCmd)
}
