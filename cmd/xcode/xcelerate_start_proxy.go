package xcode

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/invocationlog"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeversion"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcresult"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/build/bazel/remote/execution/v2"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/kv_storage"
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

			config, err := xcelerate.ReadConfig(osProxy, utils.DefaultDecoderFactory{}, utils.AllEnvs())
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
					log.WithDebugLog(common.DebugEnabled(config.DebugLogging)),
					log.WithOutput(io.MultiWriter(os.Stdout, f)),
				)

				return logger, nil
			}

			initialLogger, err := loggerFactory(initialInvocationID)
			if err != nil {
				return fmt.Errorf("failed to create initialLogger: %w", err)
			}

			initialLogger.TInfof("Xcelerate Proxy")

			return withProxySingleton(osProxy, initialLogger, func() error {
				if err := os.Remove(config.ProxySocketPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to remove socket file, error: %w", err)
				}

				initialLogger.TInfof("socketPath: %s", config.ProxySocketPath)

				signalCtx, stopSignals := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
				defer stopSignals()

				listener, err := (&net.ListenConfig{}).Listen(signalCtx, "unix", config.ProxySocketPath)
				if err != nil {
					return fmt.Errorf("failed to listen on unix socket: %w", err)
				}
				defer listener.Close()

				return StartXcodeCacheProxy(
					signalCtx,
					config,
					allEnvs,
					func(name string, v ...string) (string, error) {
						output, err := exec.Command(name, v...).Output()

						return string(output), err
					},
					nil,
					nil,
					listener,
					initialLogger,
					loggerFactory,
				)
			})
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
	initialLogger log.Logger,
	loggerFactory proxy.LoggerFactory,
) error {
	authProvider := live.Default(initialLogger).Bind(envProvider)

	client, err := common.CreateKVClient(ctx, common.CreateKVClientParams{
		CacheOperationID:   uuid.New().String(),
		ClientName:         common.ClientNameXcode,
		AuthConfig:         config.AuthConfig,
		AuthSource:         authProvider.Cached(live.HotPathTTL),
		Envs:               envProvider,
		CommandFunc:        commandFunc,
		Logger:             initialLogger,
		BitriseKVClient:    bitriseKVClient,
		EndpointURL:        config.BuildCacheEndpoint,
		CapabilitiesClient: capabilitiesClient,
		InvocationID:       initialInvocationID,
		SkipCapabilities:   true, // proxy handles capabilities calls internally
		DebugLogging:       config.DebugLogging,
	})
	if err != nil {
		return fmt.Errorf("create kv client: %w", err)
	}

	bundle := newAnalyticsBundle(ctx, config, envProvider, commandFunc, initialLogger, authProvider)

	if p, err := paths.Default(); err == nil {
		enrichment.PruneAll(p, time.Now(), initialLogger)
	} else {
		initialLogger.Debugf("Enrichment prune skipped, cannot resolve paths: %v", err)
	}

	emitter := bundle.emitter()

	sidecarOpts, peerListener := buildSidecarOptions(listener, initialLogger)
	if peerListener != nil {
		listener = peerListener
	}

	p := proxy.NewProxyWithOptions(client, config.PushEnabled, initialLogger, loggerFactory, emitter, sidecarOpts)
	p.InactivityTimeout = resolveInactivityTimeout(envProvider, initialLogger)
	p.FlushHook = func(hookCtx context.Context) []string {
		return bundle.flushEnrichmentSweep(hookCtx, initialLogger)
	}

	// Cancelled only after the final sweep so sidecars written during GracefulStop still get drained.
	enrichCtx, cancelEnrich := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelEnrich()

	if bundle.enrichmentEnabled() {
		go bundle.watcher(enrichCtx, initialLogger).Run(enrichCtx)
		go bundle.retrier(initialLogger).Run(enrichCtx)
	}

	bgCtx := context.WithoutCancel(ctx)

	go func() {
		<-ctx.Done()
		p.GracefulStop()
		bundle.finalEnrichmentSweep(bgCtx, initialLogger)
		cancelEnrich()
	}()

	serveErr := p.Serve(listener)

	p.FlushCurrentSession(bgCtx)

	//nolint:wrapcheck
	return serveErr
}

type analyticsBundle struct {
	client           *analytics.Client
	authProvider     *live.Bound
	metadata         configcommon.CacheConfigMetadata
	pending          *enrichment.Store
	handledManifests *enrichment.HandledManifestStore
	healthPath       string
	homeDir          string
	sessionsDir      string
	xcodeVersion     string
	xcodeBuildNumber string
	logger           log.Logger
}

func newAnalyticsBundle(
	ctx context.Context,
	config xcelerate.Config,
	envProvider map[string]string,
	commandFunc configcommon.CommandFunc,
	logger log.Logger,
	authProvider *live.Bound,
) *analyticsBundle {
	tokenSupplier := func() string {
		cred, origin, _ := authProvider.Resolve(context.WithoutCancel(ctx))

		return authpkg.GradleToken(cred, origin)
	}
	client, err := analytics.NewClient(consts.XcodeAnalyticsServiceEndpoint, tokenSupplier, logger)
	if err != nil {
		logger.Warnf("Xcode analytics disabled — client init failed: %s", err)

		return &analyticsBundle{logger: logger}
	}

	b := &analyticsBundle{
		client:       client,
		authProvider: authProvider,
		metadata:     configcommon.NewMetadata(envProvider, invocationUsername(envProvider), commandFunc, utils.DefaultOsProxy{}, logger),
		logger:       logger,
	}

	if version, buildNumber, err := xcodeversion.Resolve(ctx, config.OriginalXcodebuildPath, commandFunc); err != nil {
		logger.Debugf("Xcode version resolution failed — enriched invocations will omit it: %s", err)
	} else {
		b.xcodeVersion = version
		b.xcodeBuildNumber = buildNumber
	}

	if pathResolver, err := paths.Default(); err != nil {
		logger.Warnf("Pending-invocation queue disabled — paths.Default: %s", err)
	} else {
		b.pending = &enrichment.Store{Path: pathResolver.PendingInvocationsFile()}
		b.handledManifests = &enrichment.HandledManifestStore{Path: pathResolver.HandledManifestsFile()}
		b.healthPath = pathResolver.EnrichmentHealthFile()
		b.homeDir = pathResolver.Home
		b.sessionsDir = pathResolver.XcelerateSessionsDir()
	}

	return b
}

func (b *analyticsBundle) emitter() proxy.InvocationEmitter {
	if b.client == nil {
		return nil
	}

	return &slimInvocationEmitter{bundle: b}
}

func (b *analyticsBundle) enrichmentEnabled() bool {
	return b.client != nil && b.pending != nil && b.homeDir != ""
}

func (b *analyticsBundle) watcher(ctx context.Context, logger log.Logger) *enrichment.Watcher {
	return b.watcherWithEmit(ctx, logger, nil)
}

func (b *analyticsBundle) watcherWithEmit(ctx context.Context, logger log.Logger, onEmitted func(string)) *enrichment.Watcher {
	enricher := &enrichment.Enricher{
		Store:            b.pending,
		Client:           b.client,
		Auth:             b.authProvider.Get(ctx),
		Metadata:         b.metadata,
		XcodeVersion:     b.xcodeVersion,
		XcodeBuildNumber: b.xcodeBuildNumber,
		Logger:           logger,
		OnEmitted:        onEmitted,
	}
	if b.healthPath != "" {
		enricher.Health = &enrichment.HealthWriter{Path: b.healthPath}
	}
	if b.sessionsDir != "" {
		enricher.SidecarReader = enrichment.NewSidecarIndex(b.sessionsDir, logger)
	}
	enricher.XcresultParser = xcresult.NewDefaultParser(logger)
	enricher.LocalLogAppender = invocationlog.Default(logger)

	matchProbe := func(group enrichment.ManifestEntryGroup) bool {
		if b.pending == nil {
			return false
		}

		records, err := b.pending.Load()
		if err != nil {
			return false
		}

		_, matched := enrichment.Correlate(enrichment.GroupCorrelationSpan(group), records)

		return matched
	}

	return &enrichment.Watcher{
		HomeDir: b.homeDir,
		Globs: []string{
			enrichment.DefaultDerivedDataGlob,
			paths.XcodeManagedDerivedDataManifestGlobRelative,
		},
		Handle:                enricher.Enrich,
		Logger:                logger,
		MatchProbe:            matchProbe,
		MaxCorrelationRetries: enrichment.DefaultMaxCorrelationRetries,
		TimeGap:               watcherTimeGap(),
		HandledStore:          b.handledManifests,
	}
}

// watcherTimeGap widens the manifest-grouping window on CI to absorb
// wall-clock skew that a local machine doesn't have.
func watcherTimeGap() time.Duration {
	if configcommon.IsCI(utils.AllEnvs(), utils.DefaultOsProxy{}) {
		return enrichment.CIGroupTimeGap
	}

	return enrichment.LocalGroupTimeGap
}

func (b *analyticsBundle) retrier(logger log.Logger) *enrichment.Retrier {
	return &enrichment.Retrier{
		Store:  b.pending,
		Client: b.client,
		Logger: logger,
	}
}

func (b *analyticsBundle) finalEnrichmentSweep(ctx context.Context, logger log.Logger) {
	if !b.enrichmentEnabled() {
		return
	}

	b.watcher(ctx, logger).ScanOnce()
	b.retrier(logger).Sweep()
}

// flushEnrichmentSweep runs a synchronous one-shot scan and retry, returning
// the invocation IDs emitted during this sweep. Thread-safe collector so a
// background watcher goroutine stays untouched.
func (b *analyticsBundle) flushEnrichmentSweep(ctx context.Context, logger log.Logger) []string {
	if !b.enrichmentEnabled() {
		return nil
	}

	var (
		mu  sync.Mutex
		ids []string
	)
	collect := func(id string) {
		mu.Lock()
		ids = append(ids, id)
		mu.Unlock()
	}

	b.watcherWithEmit(ctx, logger, collect).ScanOnce()
	b.retrier(logger).Sweep()

	mu.Lock()
	defer mu.Unlock()

	return append([]string(nil), ids...)
}

type slimInvocationEmitter struct {
	bundle *analyticsBundle
}

func (e *slimInvocationEmitter) EmitSlim(_ context.Context, meta proxy.SessionMeta, stats proxy.SessionStats) {
	b := e.bundle

	endTime := meta.EndTime
	if endTime.IsZero() {
		endTime = time.Now()
	}
	duration := endTime.Sub(meta.StartTime).Milliseconds()
	hitRate := stats.HitRate()

	// Pending is queued for the enrichment watcher to correlate against; the
	// wrapper's own PUT is the sole writer of the rich row.
	if b.pending != nil {
		if err := b.pending.Append(enrichment.PendingRecord{
			InvocationID: meta.InvocationID,
			StartTime:    meta.StartTime,
			Duration:     duration,
			HitRate:      hitRate,
		}); err != nil {
			b.logger.Warnf("Failed to queue pending invocation %s: %s", meta.InvocationID, err)
		}
	}
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

	logDir := paths.FromHome(home).XcelerateLogDir()

	if err := paths.EnsureDir(osProxy, logDir); err != nil {
		return "", fmt.Errorf("failed to create log dir: %w", err)
	}

	return logDir, nil
}

// buildSidecarOptions sets up the per-connection sidecar writer and wraps
// the listener with the peer-PID tagger. Falls back to a no-op SidecarOptions
// (and nil wrapped listener) when the sessions dir can't be resolved.
func buildSidecarOptions(listener net.Listener, logger log.Logger) (proxy.SidecarOptions, net.Listener) {
	p, err := paths.Default()
	if err != nil {
		logger.Debugf("Sidecar writer disabled, cannot resolve paths: %v", err)

		return proxy.SidecarOptions{}, nil
	}

	dir := p.XcelerateSessionsDir()
	if err := paths.EnsureDir(utils.DefaultOsProxy{}, dir); err != nil {
		logger.Warnf("Sidecar writer disabled, cannot create %s: %v", dir, err)

		return proxy.SidecarOptions{}, nil
	}

	wrapped := proxy.NewPeerListener(listener)

	return proxy.SidecarOptions{Dir: dir, Listener: wrapped}, wrapped
}

// resolveInactivityTimeout parses TEST_BITRISE_XCELERATE_INACTIVITY_TIMEOUT off
// the injected env map. Returns zero when the var is unset, empty, or
// unparseable — zero lets proxy.inactivityDuration() fall back to its default.
func resolveInactivityTimeout(envs map[string]string, logger log.Logger) time.Duration {
	raw := envs[xcelerate.EnvInactivityTimeout]
	if raw == "" {
		return 0
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		logger.Warnf("Ignoring invalid %s=%q: %s", xcelerate.EnvInactivityTimeout, raw, err)

		return 0
	}

	return parsed
}
