// Package ccache provides a public API for the ccache-related functionality
// of bitrise-build-cache-cli. It allows Go programs to manage the ccache IPC
// server directly without executing the CLI binary.
package ccache

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	iccache "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/analytics"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// StorageHelperParams configures the ccache storage helper.
type StorageHelperParams struct {
	// InvocationID is the initial invocation ID for the session.
	// Used by Start. If empty, a new UUID is generated.
	InvocationID string

	// DebugLogging enables verbose debug output. Used by Start.
	DebugLogging bool

	// Envs is the set of environment variables to use for configuration
	// (auth tokens, workspace ID, endpoint overrides, etc.).
	// If nil, the current process environment is used.
	Envs map[string]string

	// SocketPath overrides the IPC socket path from config.
	// Used by Stop, CollectStats, HealthCheck, SetInvocationID.
	// If empty, the path from the ccache config file is used.
	SocketPath string

	// ParentInvocationID is the parent invocation ID for analytics reporting.
	// Used by Stop. If empty, falls back to BITRISE_INVOCATION_ID from Envs.
	ParentInvocationID string
}

// CollectStatsParams configures the CollectStats operation.
type CollectStatsParams struct {
	// InvocationID to report stats under (required).
	InvocationID string

	// ParentID is the parent invocation ID for the analytics report.
	ParentID string

	// DownloadedBytes is the fallback byte count if the storage helper is not running.
	// Overridden by the helper's session state when it is reachable.
	DownloadedBytes int64

	// UploadedBytes is the fallback byte count if the storage helper is not running.
	// Overridden by the helper's session state when it is reachable.
	UploadedBytes int64
}

// HealthCheckParams configures the HealthCheck operation.
type HealthCheckParams struct {
	// Timeout is how long to wait for the server to become ready.
	// Defaults to 10s if zero.
	Timeout time.Duration

	// PollInterval is how often to retry the health check.
	// Defaults to 100ms if zero.
	PollInterval time.Duration
}

// StorageHelper manages the ccache IPC storage helper lifecycle.
type StorageHelper struct {
	config  ccacheconfig.Config
	params  StorageHelperParams
	osProxy utils.OsProxy
	logger  log.Logger
}

// NewStorageHelper reads the ccache configuration from the default config path
// and returns a StorageHelper ready to Start or Stop.
func NewStorageHelper(params StorageHelperParams) (*StorageHelper, error) {
	params = withDefaults(params)

	osProxy := utils.DefaultOsProxy{}
	config, err := ccacheconfig.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
	if err != nil {
		return nil, fmt.Errorf("read ccache config: %w", err)
	}

	config.DebugLogging = config.DebugLogging || params.DebugLogging

	return &StorageHelper{
		config:  config,
		params:  params,
		osProxy: osProxy,
		logger:  log.NewLogger(log.WithDebugLog(config.DebugLogging)),
	}, nil
}

// Start connects to the Bitrise Build Cache backend and runs the IPC server
// that proxies ccache secondary storage requests. It blocks until ctx is
// cancelled or the configured idle timeout is reached.
func (h *StorageHelper) Start(ctx context.Context) error {
	kvClient, err := createKVClient(ctx, h.config, h.params.Envs, h.params.InvocationID)
	if err != nil {
		return fmt.Errorf("create KV client: %w", err)
	}

	logger, err := h.createLogger(h.params.InvocationID)
	if err != nil {
		return fmt.Errorf("create initial logger: %w", err)
	}
	kvClient.SetLogger(logger)

	logger.TInfof("Ccache storage helper")
	logger.TInfof("socketPath: %s", h.config.IPCEndpoint)

	metadata := configcommon.CacheConfigMetadata{
		BitriseAppID:           h.params.Envs["BITRISE_APP_SLUG"],
		BitriseBuildID:         h.params.Envs["BITRISE_BUILD_SLUG"],
		BitriseStepExecutionID: h.params.Envs["BITRISE_STEP_EXECUTION_ID"],
	}

	server, err := iccache.NewServer(
		h.config,
		metadata,
		kvClient,
		logger,
		h.createLogger,
		h.params.InvocationID,
	)
	if err != nil {
		return fmt.Errorf("create IPC server: %w", err)
	}

	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("run IPC server: %w", err)
	}

	return nil
}

// Stop gracefully shuts down a running storage helper, collects and reports
// analytics, then registers the invocation relation. Returns nil without
// error if the helper is not running.
func (h *StorageHelper) Stop(ctx context.Context) error {
	socketPath := h.socketPath()

	if !iccache.IsListening(socketPath) { //nolint:contextcheck // IsListening uses its own short-lived context
		h.logger.TInfof("Storage helper is not running, nothing to stop")

		return nil
	}

	parentInvocationID := resolveParentInvocationID(h.params.ParentInvocationID, h.params.Envs)
	ccacheInvocationID := uuid.New().String()

	dl, ul, err := iccache.SendGetSessionStats(ctx, socketPath)
	if err != nil {
		h.logger.TWarnf("Failed to get session stats from storage helper: %v", err)
	}

	if err := iccache.SendStop(ctx, socketPath); err != nil {
		return fmt.Errorf("send stop to storage helper: %w", err)
	}

	collectAndZeroCcacheStats(ctx, h.config, ccacheInvocationID, parentInvocationID, dl, ul, h.logger)

	if parentInvocationID != "" {
		registerInvocationRelation(h.config, parentInvocationID, ccacheInvocationID, h.logger)
	}

	return nil
}

// CollectStats collects and reports ccache statistics, then zeros the counters.
// If the storage helper is reachable, its session byte counts override the
// values provided in params.
func (h *StorageHelper) CollectStats(ctx context.Context, params CollectStatsParams) error {
	if params.InvocationID == "" {
		return fmt.Errorf("invocation ID is required")
	}

	socketPath := h.socketPath()
	dl, ul := params.DownloadedBytes, params.UploadedBytes

	if iccache.IsListening(socketPath) { //nolint:contextcheck // IsListening uses its own short-lived context
		sessionDL, sessionUL, err := iccache.SendGetSessionStats(ctx, socketPath)
		if err != nil {
			h.logger.TWarnf("Failed to get session stats from storage helper: %v", err)
		} else {
			dl, ul = sessionDL, sessionUL
		}
	}

	collectAndZeroCcacheStats(ctx, h.config, params.InvocationID, params.ParentID, dl, ul, h.logger)

	return nil
}

// HealthCheck polls the storage helper until it responds or the timeout expires.
func (h *StorageHelper) HealthCheck(ctx context.Context, params HealthCheckParams) error {
	params = withHealthCheckDefaults(params)
	socketPath := h.socketPath()

	deadline := time.Now().Add(params.Timeout)

	for {
		if err := iccache.SendHealthCheck(ctx, socketPath); err == nil {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("storage helper did not become ready within %s", params.Timeout)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("health check cancelled: %w", ctx.Err())
		case <-time.After(params.PollInterval):
		}
	}
}

// SetInvocationID sends a parent→child invocation ID pair to the running
// storage helper via IPC.
func (h *StorageHelper) SetInvocationID(ctx context.Context, parentID, childID string) error {
	socketPath := h.socketPath()

	if err := iccache.SendInvocationID(ctx, socketPath, parentID, childID); err != nil {
		return fmt.Errorf("send invocation ID: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Private — StorageHelper methods
// ---------------------------------------------------------------------------

func (h *StorageHelper) socketPath() string {
	if h.params.SocketPath != "" {
		return h.params.SocketPath
	}

	return h.config.IPCEndpoint
}

func (h *StorageHelper) createLogger(invocationID string) (log.Logger, error) {
	logFile, err := h.logFilePath(invocationID)
	if err != nil {
		return nil, fmt.Errorf("get log file: %w", err)
	}

	f, err := h.osProxy.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", logFile, err)
	}

	return log.NewLogger(
		log.WithDebugLog(h.config.DebugLogging),
		log.WithOutput(io.MultiWriter(os.Stdout, f)),
	), nil
}

func (h *StorageHelper) logDir() (string, error) {
	home, err := h.osProxy.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home dir: %w", err)
	}

	logDir := filepath.Join(home, ".local", "state", "ccache", "logs")

	if err := h.osProxy.MkdirAll(logDir, 0o755); err != nil {
		return "", fmt.Errorf("create log dir: %w", err)
	}

	return logDir, nil
}

func (h *StorageHelper) logFilePath(invocationID string) (string, error) {
	dir, err := h.logDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, fmt.Sprintf(h.config.LogFile, invocationID)), nil
}

// ---------------------------------------------------------------------------
// Private — package-level helpers
// ---------------------------------------------------------------------------

func withHealthCheckDefaults(params HealthCheckParams) HealthCheckParams {
	if params.Timeout == 0 {
		params.Timeout = 10 * time.Second
	}

	if params.PollInterval == 0 {
		params.PollInterval = 100 * time.Millisecond
	}

	return params
}

func withDefaults(params StorageHelperParams) StorageHelperParams {
	if params.InvocationID == "" {
		params.InvocationID = uuid.NewString()
	}

	if params.Envs == nil {
		params.Envs = utils.AllEnvs()
	}

	return params
}

func resolveParentInvocationID(flagValue string, envs map[string]string) string {
	if flagValue != "" {
		return flagValue
	}

	return envs["BITRISE_INVOCATION_ID"]
}

func collectAndZeroCcacheStats(ctx context.Context, config ccacheconfig.Config, invocationID, parentID string, downloadedBytes, uploadedBytes int64, logger log.Logger) {
	client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
	if err != nil {
		logger.TErrorf("Skipping ccache stats collection because analytics client creation failed: %v", err)

		return
	}

	ccacheanalytics.CollectAndZero(ctx, client, invocationID, parentID, downloadedBytes, uploadedBytes, logger)
}

func registerInvocationRelation(config ccacheconfig.Config, parentID, childID string, logger log.Logger) {
	client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
	if err != nil {
		logger.TErrorf("Failed to create analytics client for invocation relation: %v", err)

		return
	}

	rel := multiplatform.InvocationRelation{
		ParentInvocationID: parentID,
		ChildInvocationID:  childID,
		InvocationDate:     time.Now(),
		BuildTool:          "ccache",
	}

	if err := client.PutInvocationRelation(rel); err != nil {
		logger.TErrorf("Failed to register invocation relation (parent=%s child=%s): %v", parentID, childID, err)
	}
}

func createKVClient(
	ctx context.Context,
	config ccacheconfig.Config,
	envs map[string]string,
	invocationID string,
) (*kv.Client, error) {
	endpointURL := configcommon.SelectCacheEndpointURL(config.BuildCacheEndpoint, envs)

	buildCacheHost, insecureGRPC, err := kv.ParseURLGRPC(endpointURL)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint URL %q: %w", endpointURL, err)
	}

	logger := log.NewLogger(log.WithDebugLog(config.DebugLogging))
	commandFunc := newCommandFunc(ctx)

	client, err := kv.NewClient(kv.NewClientParams{
		UseInsecure:         insecureGRPC,
		Host:                buildCacheHost,
		DialTimeout:         5 * time.Second,
		ClientName:          "ccache",
		AuthConfig:          config.AuthConfig,
		Logger:              logger,
		CacheConfigMetadata: configcommon.NewMetadata(envs, commandFunc, logger),
		CacheOperationID:    uuid.NewString(),
		InvocationID:        invocationID,
	})
	if err != nil {
		return nil, fmt.Errorf("new KV client: %w", err)
	}

	if err := client.GetCapabilitiesWithRetry(ctx); err != nil {
		return nil, fmt.Errorf("get capabilities: %w", err)
	}

	return client, nil
}

func newCommandFunc(ctx context.Context) configcommon.CommandFunc {
	return func(name string, args ...string) (string, error) {
		output, err := exec.CommandContext(ctx, name, args...).Output() //nolint:gosec

		return string(output), err
	}
}
