package xcode

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/proxy"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/kv_storage"
)

//go:generate moq -rm -stub -pkg mocks -out ./mocks/kv_storage.go ./../../proto/kv_storage KVStorageClient
//go:generate moq -rm -stub -pkg mocks -out ./mocks/remote_execution.go ./../../proto/build/bazel/remote/execution/v2 CapabilitiesClient

var (
	//nolint:gochecknoglobals
	initialInvocationID string

	//nolint:gochecknoglobals
	xcelerateProxyCmd = &cobra.Command{
		Use:          "start-proxy",
		Short:        "Start Xcelerate Proxy",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := log.NewLogger()
			logger.TInfof("Xcelerate Proxy")

			allEnvs := utils.AllEnvs()

			config, err := xcelerate.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
			if err != nil {
				return fmt.Errorf("read xcelerate config: %w", err)
			}

			logger.EnableDebugLog(config.DebugLogging)

			if err := os.Remove(config.ProxySocketPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove socket file, error: %w", err)
			}

			logger.TInfof("socketPath: %s", config.ProxySocketPath)

			listener, err := (&net.ListenConfig{}).Listen(cmd.Context(), "unix", config.ProxySocketPath)
			if err != nil {
				return fmt.Errorf("failed to listen on unix socket: %w", err)
			}
			defer listener.Close()

			return StartXcodeCacheProxy(
				cmd.Context(),
				config.AuthConfig,
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
)

func init() {
	xcelerateCommand.Flags().StringVar(
		&initialInvocationID,
		"invocation-id",
		uuid.NewString(),
		"Invocation ID to be used in the proxy",
	)
	xcelerateCommand.AddCommand(xcelerateProxyCmd)
}

func StartXcodeCacheProxy(
	ctx context.Context,
	auth configcommon.CacheAuthConfig,
	envProvider map[string]string,
	commandFunc configcommon.CommandFunc,
	bitriseKVClient kv_storage.KVStorageClient,
	capabilitiesClient remoteexecution.CapabilitiesClient,
	listener net.Listener,
	logger log.Logger,
) error {
	client, err := common.CreateKVClient(ctx, common.CreateKVClientParams{
		CacheOperationID:   uuid.New().String(),
		ClientName:         common.ClientNameXcode,
		AuthConfig:         auth,
		Envs:               envProvider,
		CommandFunc:        commandFunc,
		Logger:             logger,
		BitriseKVClient:    bitriseKVClient,
		CapabilitiesClient: capabilitiesClient,
		InvocationID:       initialInvocationID,
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
