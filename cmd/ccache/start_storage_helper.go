package ccache

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/proxy"
)

//nolint:gochecknoglobals
var (
	initialInvocationID string

	ccacheCmd = &cobra.Command{
		Use:          "ccache",
		Short:        "Ccache related commands",
		SilenceUsage: true,
	}

	storageHelperCmd = &cobra.Command{
		Use:          "storage-helper",
		Short:        "Ccache storage helper",
		SilenceUsage: true,
	}

	startStorageHelperCmd = &cobra.Command{
		Use:          "start",
		Short:        "Start Xcelerate's ccache storage helper",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			osProxy := utils.DefaultOsProxy{}
			config, err := ccacheconfig.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
			if err != nil {
				return fmt.Errorf("read ccache config: %w", err)
			}

			envs := utils.AllEnvs()
			kvClient, err := createKVClient(
				cmd.Context(),
				config,
				envs,
				initialInvocationID,
				func(name string, v ...string) (string, error) {
					output, err := exec.Command(name, v...).Output()

					return string(output), err
				},
			)
			if err != nil {
				return fmt.Errorf("failed to create KV client: %w", err)
			}

			ccacheStorageHelper, err := newCcacheStorageHelper(
				config,
				configcommon.CacheConfigMetadata{
					BitriseAppID:           envs["BITRISE_APP_SLUG"],
					BitriseBuildID:         envs["BITRISE_BUILD_SLUG"],
					BitriseStepExecutionID: envs["BITRISE_STEP_EXECUTION_ID"],
				},
				osProxy,
				initialInvocationID,
				kvClient,
			)
			if err != nil {
				return fmt.Errorf("failed to create ccache storage helper: %w", err)
			}

			errWriter, err := ccacheStorageHelper.getErrorWriter()
			if err != nil {
				return fmt.Errorf("failed to get error writer: %w", err)
			}
			cmd.SetErr(errWriter)

			return ccacheStorageHelper.start(cmd.Context())
		},
	}
)

func init() {
	startStorageHelperCmd.Flags().StringVar(
		&initialInvocationID,
		"invocation-id",
		uuid.NewString(),
		"Invocation ID to be used in the proxy",
	)

	common.RootCmd.AddCommand(ccacheCmd)
	ccacheCmd.AddCommand(storageHelperCmd)
	storageHelperCmd.AddCommand(startStorageHelperCmd)
}

func createKVClient(
	ctx context.Context,
	config ccacheconfig.Config,
	envs map[string]string,
	invocationID string,
	commandFunc configcommon.CommandFunc,
) (proxy.Client, error) {
	client, err := common.CreateKVClient(ctx, common.CreateKVClientParams{
		CacheOperationID: uuid.New().String(),
		ClientName:       common.ClientNameCcache,
		AuthConfig:       config.AuthConfig,
		Envs:             envs,
		CommandFunc:      commandFunc,
		Logger:           log.NewLogger(),
		EndpointURL:      config.BuildCacheEndpoint,
		InvocationID:     invocationID,
		SkipCapabilities: false, // handled by the storage helper itself, no need for the client to fetch capabilities
	})
	if err != nil {
		return nil, fmt.Errorf("create KV client: %w", err)
	}

	return client, nil
}

type ccacheStorageHelper struct {
	osProxy         utils.OsProxy
	config          ccacheconfig.Config
	logger          log.Logger
	bitriseKVClient proxy.Client
	server          *ccache.IpcServer

	loggerFactory func(c *ccacheStorageHelper, invocationID string, verbose bool) (log.Logger, error)
	start         func(ctx context.Context) error
}

func newCcacheStorageHelper(
	config ccacheconfig.Config,
	metadata configcommon.CacheConfigMetadata,
	osProxy utils.OsProxy,
	invocationID string,
	kvClient proxy.Client,
) (*ccacheStorageHelper, error) {
	helper := &ccacheStorageHelper{
		config:        config,
		osProxy:       osProxy,
		loggerFactory: defaultLoggerFactory,
	}

	// Set up logger early so that any errors can be logged to file instead of just stderr
	logger, err := helper.loggerFactory(helper, invocationID, common.IsDebugLogMode)
	if err != nil {
		return nil, fmt.Errorf("failed to create initial logger: %w", err)
	}
	helper.logger = logger
	kvClient.SetLogger(logger)
	helper.bitriseKVClient = kvClient

	logger.TInfof("Ccache storage helper")
	logger.TInfof("socketPath: %s", config.IPCEndpoint)

	helper.server, err = ccache.NewServer(
		config,
		metadata,
		kvClient,
		helper.logger,
		func(invocationID string) (log.Logger, error) {
			return helper.loggerFactory(helper, invocationID, common.IsDebugLogMode)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create IPC server: %w", err)
	}

	helper.start = helper.server.Run

	return helper, nil
}

func (c *ccacheStorageHelper) getErrorWriter() (io.Writer, error) {
	proxyErrorLogFile, err := c.getProxyErrorLogFile()
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy error log file: %w", err)
	}

	errFile, err := c.osProxy.OpenFile(proxyErrorLogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open proxy error log file (%s), error: %w", proxyErrorLogFile, err)
	}

	return io.MultiWriter(os.Stderr, errFile), nil
}

func (c *ccacheStorageHelper) getLogDir() (string, error) {
	home, err := c.osProxy.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home dir: %w", err)
	}

	logDir := fmt.Sprintf("%s/.local/state/ccache/logs", home)

	if err := c.osProxy.MkdirAll(logDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create log dir: %w", err)
	}

	return logDir, nil
}

func (c *ccacheStorageHelper) getProxyErrorLogFile() (string, error) {
	logDir, err := c.getLogDir()
	if err != nil {
		return "", fmt.Errorf("failed to get log dir: %w", err)
	}

	return filepath.Join(logDir, c.config.ErrLogFile), nil
}

func (c *ccacheStorageHelper) getLogFile(invocationID string) (string, error) {
	logDir, err := c.getLogDir()
	if err != nil {
		return "", fmt.Errorf("failed to get log dir: %w", err)
	}

	return filepath.Join(logDir, fmt.Sprintf(c.config.LogFile, invocationID)), nil
}

func defaultLoggerFactory(c *ccacheStorageHelper, invocationID string, verbose bool) (log.Logger, error) {
	logFile, err := c.getLogFile(invocationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get log file: %w", err)
	}

	f, err := c.osProxy.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file (%s), error: %w", logFile, err)
	}

	logger := log.NewLogger(
		log.WithDebugLog(verbose),
		log.WithOutput(io.MultiWriter(os.Stdout, f)),
	)

	return logger, nil
}
