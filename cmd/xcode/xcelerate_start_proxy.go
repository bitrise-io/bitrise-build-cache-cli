package xcode

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"

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

const (
	proxyOut = "proxy-%s-out.log"
	proxyErr = "proxy-err.log"
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
			osProxy := utils.DefaultOsProxy{}

			proxyErrorLogFile, err := getProxyErrorLogFile(osProxy)
			if err != nil {
				return fmt.Errorf("failed to get proxy error log file: %w", err)
			}

			errFile, err := osProxy.OpenFile(proxyErrorLogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("failed to open proxy error log file (%s), error: %w", proxyErrorLogFile, err)
			}
			cmd.SetErr(io.MultiWriter(os.Stderr, errFile))

			config, err := xcelerate.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
			if err != nil {
				return fmt.Errorf("read xcelerate config: %w", err)
			}

			allEnvs := utils.AllEnvs()

			loggerFactory := func(invocationID string) (log.Logger, error) {
				proxyLogFile, err := getProxyLogFile(osProxy, invocationID)
				if err != nil {
					return nil, fmt.Errorf("failed to get proxy log file: %w", err)
				}

				f, err := osProxy.OpenFile(proxyLogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
				if err != nil {
					return nil, fmt.Errorf("failed to open proxy log file (%s), error: %w", proxyLogFile, err)
				}

				logger := log.NewLogger(
					log.WithDebugLog(config.DebugLogging),
					log.WithOutput(io.MultiWriter(os.Stdout, f)),
				)

				return logger, nil
			}

			logger, err := loggerFactory(initialInvocationID)
			if err != nil {
				return fmt.Errorf("failed to create logger: %w", err)
			}

			logger.TInfof("Xcelerate Proxy")

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
				config,
				allEnvs,
				func(name string, v ...string) (string, error) {
					output, err := exec.Command(name, v...).Output()

					return string(output), err
				},
				nil,
				nil,
				listener,
				logger,
				loggerFactory,
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
	config xcelerate.Config,
	envProvider map[string]string,
	commandFunc configcommon.CommandFunc,
	bitriseKVClient kv_storage.KVStorageClient,
	capabilitiesClient remoteexecution.CapabilitiesClient,
	listener net.Listener,
	logger log.Logger,
	loggerFactory proxy.LoggerFactory,
) error {
	client, err := common.CreateKVClient(ctx, common.CreateKVClientParams{
		CacheOperationID:   uuid.New().String(),
		ClientName:         common.ClientNameXcode,
		AuthConfig:         config.AuthConfig,
		Envs:               envProvider,
		CommandFunc:        commandFunc,
		Logger:             logger,
		BitriseKVClient:    bitriseKVClient,
		EndpointURL:        config.BuildCacheEndpoint,
		CapabilitiesClient: capabilitiesClient,
		InvocationID:       initialInvocationID,
		SkipCapabilities:   true, // proxy handles capabilities calls internally
	})
	if err != nil {
		return fmt.Errorf("create kv client: %w", err)
	}

	//nolint:wrapcheck
	return proxy.
		NewProxy(client, logger, loggerFactory).
		Serve(listener)
}

func getProxyLogFile(osProxy utils.OsProxy, invocationID string) (string, error) {
	logDir, err := getLogDir(osProxy)
	if err != nil {
		return "", fmt.Errorf("failed to get log dir: %w", err)
	}

	return filepath.Join(logDir, fmt.Sprintf(proxyOut, invocationID)), nil
}

func getProxyErrorLogFile(osProxy utils.OsProxy) (string, error) {
	logDir, err := getLogDir(osProxy)
	if err != nil {
		return "", fmt.Errorf("failed to get log dir: %w", err)
	}

	return filepath.Join(logDir, proxyErr), nil
}

func getLogDir(osProxy utils.OsProxy) (string, error) {
	home, err := osProxy.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home dir: %w", err)
	}

	logDir := fmt.Sprintf("%s/.local/state/xcelerate/logs", home)

	if err := osProxy.MkdirAll(logDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create log dir: %w", err)
	}

	return logDir, nil
}
