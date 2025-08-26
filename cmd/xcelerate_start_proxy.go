package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/proxy"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/kv_storage"
)

//go:generate moq -rm -stub -pkg mocks -out ./mocks/kv_storage.go ./../proto/kv_storage KVStorageClient
//go:generate moq -rm -stub -pkg mocks -out ./mocks/remote_execution.go ./../proto/build/bazel/remote/execution/v2 CapabilitiesClient

// This command should go under an xcelerate subcommand together with stop-xcode-proxy
var xcelerateProxyCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:          "start-proxy",
	Short:        "Start Xcelerate Proxy",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Xcelerate Proxy")

		allEnvs := utils.AllEnvs()
		socketPath := allEnvs["BITRISE_XCELERATE_SOCKET_PATH"]
		if socketPath == "" {
			socketPath = filepath.Join(os.TempDir(), "xcelerate-proxy.sock")
		}

		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove socket file, error: %w", err)
		}

		logger.TInfof("socketPath: %s", socketPath)

		listener, err := (&net.ListenConfig{}).Listen(cmd.Context(), "unix", socketPath)
		if err != nil {
			return fmt.Errorf("failed to listen on unix socket: %w", err)
		}
		defer listener.Close()

		authConfig, err := common.ReadAuthConfigFromEnvironments(allEnvs)
		if err != nil {
			return fmt.Errorf("read auth config from environments: %w", err)
		}

		return StartXcodeCacheProxy(
			cmd.Context(),
			authConfig,
			allEnvs,
			func(name string, v ...string) (string, error) {
				output, err := exec.Command(name, v...).Output()

				return string(output), err
			},
			nil,
			nil,
			listener,
			logger,
		)
	},
}

func init() {
	xcelerateCommand.AddCommand(xcelerateProxyCmd)
}

func StartXcodeCacheProxy(
	ctx context.Context,
	auth common.CacheAuthConfig,
	envProvider map[string]string,
	commandFunc common.CommandFunc,
	bitriseKVClient kv_storage.KVStorageClient,
	capabilitiesClient remoteexecution.CapabilitiesClient,
	listener net.Listener,
	logger log.Logger,
) error {
	client, err := createKVClient(ctx, CreateKVClientParams{
		CacheOperationID:   uuid.New().String(),
		ClientName:         ClientNameXcelerate,
		AuthConfig:         auth,
		Envs:               envProvider,
		CommandFunc:        commandFunc,
		Logger:             logger,
		BitriseKVClient:    bitriseKVClient,
		CapabilitiesClient: capabilitiesClient,
		InvocationID:       envProvider["INVOCATION_ID"],
		SkipCapabilities:   true, // proxy handles capabilities calls internally
	})
	if err != nil {
		return fmt.Errorf("create kv client: %w", err)
	}

	//nolint:wrapcheck
	return proxy.
		NewProxy(client, logger).
		Serve(listener)
}
