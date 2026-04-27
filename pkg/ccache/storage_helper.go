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
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/build_cache/kv"
	iccache "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/ccache"
	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/ccache/analytics"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
	pkgcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/common/childstats"
)

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// StorageHelperParams configures the ccache storage helper.
type StorageHelperParams struct {
	// InvocationID is the initial invocation ID for the session.
	// Used by Start. If empty, a new UUID is generated.
	InvocationID string

	// ParentInvocationID is the initial parent invocation ID.
	// If empty, withDefaults falls back to BITRISE_INVOCATION_ID from Envs.
	ParentInvocationID string

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
	config   ccacheconfig.Config
	params   StorageHelperParams
	osProxy  utils.OsProxy
	logger   log.Logger
	registry *pkgcommon.InvocationRegistry

	// Session state
	sessionMu    sync.RWMutex
	invocationID string
	parentID     string
	downloaded   int64
	uploaded     int64
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

	registry, err := pkgcommon.NewInvocationRegistry(pkgcommon.InvocationRegistryParams{
		Envs: params.Envs,
	})
	if err != nil {
		return nil, fmt.Errorf("create invocation registry: %w", err)
	}

	return &StorageHelper{
		config:   config,
		params:   params,
		osProxy:  osProxy,
		logger:   log.NewLogger(log.WithDebugLog(config.DebugLogging)),
		registry: registry,

		invocationID: params.InvocationID,
		parentID:     params.ParentInvocationID,
		uploaded:     0,
		downloaded:   0,
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

// Stop gracefully shuts down a running storage helper. Returns nil without
// error if the helper is not running. Only stops the process — does not
// collect or send analytics. Use CollectAndSendStats separately.
func (h *StorageHelper) Stop(ctx context.Context) error {
	socketPath := h.socketPath()

	if !iccache.IsListening(socketPath) { //nolint:contextcheck // IsListening uses its own short-lived context
		h.logger.TInfof("Storage helper is not running, nothing to stop")

		return nil
	}

	if err := iccache.SendStop(ctx, socketPath); err != nil {
		return fmt.Errorf("send stop to storage helper: %w", err)
	}

	return nil
}

// RegisterInvocationRelation records the parent→child invocation relation
// using the IDs from internal state (set at construction or via SetInvocationID).
// Errors are logged but do not fail the caller. No-op if parentID is empty.
func (h *StorageHelper) registerInvocationRelation(ctx context.Context) {
	h.sessionMu.RLock()
	parentID := h.parentID
	childID := h.invocationID
	h.sessionMu.RUnlock()

	if parentID == "" {
		h.logger.TInfof("No parent invocation ID available, skipping invocation relation registration")

		return
	}

	if err := h.registry.RegisterRelation(ctx, pkgcommon.RegisterRelationParams{
		ParentID:  parentID,
		ChildID:   childID,
		BuildTool: "ccache",
	}); err != nil {
		h.logger.TWarnf("Failed to register invocation relation: %v", err)
	}
}

// CollectAndSendStats collects ccache statistics and, if ccache had any activity,
// reports the ccache invocation and registers the parent→child relationship.
// Always zeros ccache counters at the end regardless of activity.
// If the storage helper is reachable, its session byte counts and active invocation
// IDs override the values from internal state and params.
func (h *StorageHelper) CollectAndSendStats(ctx context.Context, invocationIDOverride, parentIDOverride string) {
	defer h.zeroCcacheStats(ctx, h.logger)

	_, err := h.loadSessionInfo(ctx, invocationIDOverride, parentIDOverride)
	if err != nil {
		h.logger.TWarnf("Failed to load session info from storage helper, stats collection will be skipped: %v", err)

		return
	}

	var stats ccacheanalytics.CcacheStats
	if s, err := h.parseCcacheStats(ctx); err != nil {
		h.logger.TWarnf("Failed to parse ccache stats, reporting transfer bytes only: %v", err)
	} else {
		stats = s
	}

	if c, err := h.parseCcacheConfig(ctx); err != nil {
		h.logger.TWarnf("Failed to parse ccache config: %v", err)
	} else {
		stats.Config = c
	}

	h.sessionMu.RLock()
	dl, ul := h.downloaded, h.uploaded
	invocationID := h.invocationID
	parentID := h.parentID
	h.sessionMu.RUnlock()

	hasActivity := stats.HasActivity() || dl > 0 || ul > 0
	if !hasActivity {
		h.logger.TInfof("No ccache activity detected, skipping analytics")

		return
	}

	if invocationID == "" {
		h.logger.TWarnf("No invocation ID available for ccache stats, skipping analytics")

		return
	}

	h.logger.TInfof("Ccache invocation ID: %s", invocationID)
	h.logger.TInfof("Parent invocation ID: %s", parentID)

	client, err := ccacheanalytics.NewClient(consts.MultiplatformAnalyticsServiceEndpoint, h.config.AuthConfig.TokenInGradleFormat(), h.logger)
	if err != nil {
		h.logger.TWarnf("Failed to create analytics client for ccache stats: %v", err)

		return
	}

	h.registerInvocationRelation(ctx)

	metadata := configcommon.NewMetadata(h.params.Envs, newCommandFunc(ctx), h.logger)

	inv := ccacheanalytics.NewCcacheInvocation(invocationID, parentID, time.Now(), stats, dl, ul, h.config.AuthConfig, metadata)
	if err := client.PutCcacheInvocation(*inv); err != nil {
		h.logger.TWarnf("Failed to send ccache invocation: %v", err)
	}

	h.writeChildStatsLedger(invocationID, parentID, stats)
}

// writeChildStatsLedger records this ccache invocation's hit rate in the
// parent's local ledger so a parent wrapper (e.g. react-native) can
// aggregate child hit rates at the end of its run. No-op when no parent.
func (h *StorageHelper) writeChildStatsLedger(invocationID, parentID string, stats ccacheanalytics.CcacheStats) {
	if parentID == "" {
		return
	}

	total := int64(stats.CacheHit + stats.CacheMiss)

	entry := childstats.Entry{
		ChildInvocationID:  invocationID,
		ParentInvocationID: parentID,
		BuildTool:          "ccache",
		HitRate:            float32(stats.CacheHitRate),
		Hits:               int64(stats.CacheHit),
		Total:              total,
		BenchmarkPhase:     os.Getenv(configcommon.BenchmarkPhaseEnvVar("ccache")),
	}

	if err := childstats.NewWriter().Write(entry); err != nil {
		h.logger.TWarnf("Failed to write child stats ledger: %v", err)
	}
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
// storage helper via IPC, then updates the internal ID state.
// State is only updated on success.
func (h *StorageHelper) SetInvocationID(ctx context.Context, parentID, childID string) error {
	socketPath := h.socketPath()

	if err := iccache.SendInvocationID(ctx, socketPath, parentID, childID); err != nil {
		return fmt.Errorf("send invocation ID: %w", err)
	}

	h.sessionMu.Lock()
	h.parentID = parentID
	h.invocationID = childID
	h.sessionMu.Unlock()

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

func (h *StorageHelper) loadSessionInfo(ctx context.Context, invocationIDOverride, parentIDOverride string) (iccache.SessionStats, error) {
	socketPath := h.socketPath()

	if !iccache.IsListening(socketPath) { //nolint:contextcheck // IsListening uses its own short-lived context
		h.logger.TInfof("Storage helper is not running, no session info available")

		return iccache.SessionStats{}, fmt.Errorf("storage helper is not running")
	}

	stats, err := iccache.SendGetSessionStats(ctx, socketPath)
	if err != nil {
		h.logger.TWarnf("Failed to get session stats from storage helper: %v", err)

		return iccache.SessionStats{}, fmt.Errorf("failed to get session stats: %w", err)
	}

	// Update internal state with loaded session info, allowing overrides from params.
	// This ensures the caller can correlate the session info with the correct invocation IDs
	// even if the helper was running with different IDs or the caller wants to override them for analytics purposes.
	h.sessionMu.Lock()
	switch {
	case invocationIDOverride != "":
		h.invocationID = invocationIDOverride
	case stats.InvocationID != "":
		h.invocationID = stats.InvocationID
	}

	switch {
	case parentIDOverride != "":
		h.parentID = parentIDOverride
	case stats.ParentID != "":
		h.parentID = stats.ParentID
	}

	h.uploaded = stats.UploadedBytes
	h.downloaded = stats.DownloadedBytes
	h.sessionMu.Unlock()

	return stats, nil
}

func (h *StorageHelper) parseCcacheStats(ctx context.Context) (ccacheanalytics.CcacheStats, error) {
	ccachePath, err := exec.LookPath("ccache")
	if err != nil {
		return ccacheanalytics.CcacheStats{}, fmt.Errorf("ccache binary not found: %w", err)
	}

	statsData, err := exec.CommandContext(ctx, ccachePath, "-v", "-v", "-s").Output() //nolint:gosec
	if err != nil {
		return ccacheanalytics.CcacheStats{}, fmt.Errorf("run ccache -v -v -s: %w", err)
	}

	stats, err := ccacheanalytics.ParseCcacheStats(statsData)
	if err != nil {
		return ccacheanalytics.CcacheStats{}, fmt.Errorf("parse ccache stats: %w", err)
	}

	return stats, nil
}

func (h *StorageHelper) parseCcacheConfig(ctx context.Context) ([]ccacheanalytics.CcacheConfigEntry, error) {
	ccachePath, err := exec.LookPath("ccache")
	if err != nil {
		return nil, fmt.Errorf("ccache binary not found: %w", err)
	}

	configData, err := exec.CommandContext(ctx, ccachePath, "--show-config").Output() //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("run ccache --show-config: %w", err)
	}

	return ccacheanalytics.ParseCcacheConfig(configData), nil
}

func (h *StorageHelper) zeroCcacheStats(ctx context.Context, logger log.Logger) {
	ccachePath, err := exec.LookPath("ccache")
	if err != nil {
		return
	}

	if err := exec.CommandContext(ctx, ccachePath, "-z").Run(); err != nil { //nolint:gosec
		logger.TErrorf("Failed to reset ccache stats: %v", err)
	}
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
	if params.Envs == nil {
		params.Envs = utils.AllEnvs()
	}

	if params.InvocationID == "" {
		params.InvocationID = uuid.NewString()
	}

	if params.ParentInvocationID == "" {
		params.ParentInvocationID = params.Envs["BITRISE_INVOCATION_ID"]
	}

	return params
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
