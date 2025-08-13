package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate_proxy"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/kv_storage"
)

var xcelerateProxyCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:          "xcode-proxy",
	Short:        "Xcode Cache Proxy",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Xcode Cache Proxy")

		return startXcodeCacheProxy(
			cmd.Context(),
			logger,
			os.Getenv,
			utils.DefaultOsProxy{},
		)
	},
}

func init() {
	rootCmd.AddCommand(xcelerateProxyCmd)
}

func startXcodeCacheProxy(ctx context.Context, logger log.Logger, envProvider func(string) string, osProxy utils.OsProxy) error {
	socketPath := envProvider("SOCKET_PATH")
	if socketPath == "" {
		socketPath = filepath.Join(os.TempDir(), "xcelerate-proxy.sock")
	}

	logger.TInfof("socketPath: %s", socketPath)

	targetAddr := envProvider("TARGET_ADDR")
	if targetAddr == "" {
		return errors.New("no TARGET_ADDR specified")
	}

	invocationID := envProvider("INVOCATION_ID")
	token := envProvider("REMOTE_CACHE_TOKEN")
	appSlug := envProvider("BITRISE_APP_SLUG")
	orgID := envProvider("BITRISE_ORG_ID")
	buildSlug := envProvider("BITRISE_BUILD_SLUG")
	stepExecutionID := envProvider("BITRISE_STEP_EXECUTION_ID")

	if err := osProxy.Remove(socketPath); err != nil {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket: %w", err)
	}

	client, err := kv.NewGRPCClient(targetAddr)
	if err != nil {
		return fmt.Errorf("failed to create proxy: %w", err)
	}

	grpcServer := xcelerate_proxy.NewProxy(
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

	return grpcServer.Serve(listener) //nolint:wrapcheck
}
