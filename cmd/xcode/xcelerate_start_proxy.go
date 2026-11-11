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
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/invocationlog"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/wrapperless"
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
	wrapperlessPipelineEnabled bool

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
	xcelerateProxyCmd.Flags().BoolVar(
		&wrapperlessPipelineEnabled,
		"wrapperless-pipeline-enabled",
		false,
		"Run the opt-in wrapperless single-ID pipeline alongside the existing enricher",
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
		Compression: kv.CompressionParams{
			Enabled:   config.RemoteCacheCompression,
			Threshold: config.RemoteCacheCompressionThreshold,
		},
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

	var emitter proxy.InvocationEmitter
	if !wrapperlessPipelineEnabled {
		emitter = bundle.emitter()
	}

	sidecarOpts, peerListener := buildSidecarOptions(listener, initialLogger)
	if peerListener != nil {
		listener = peerListener
	}

	p := proxy.NewProxyWithOptions(client, config.PushEnabled, initialLogger, loggerFactory, emitter, sidecarOpts)
	p.InactivityTimeout = resolveInactivityTimeout(envProvider, initialLogger)
	if !wrapperlessPipelineEnabled {
		p.FlushHook = func(hookCtx context.Context) []string {
			return bundle.flushEnrichmentSweep(hookCtx, initialLogger)
		}
	}

	// Cancelled only after the final sweep so sidecars written during GracefulStop still get drained.
	enrichCtx, cancelEnrich := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelEnrich()

	if bundle.enrichmentEnabled() && !wrapperlessPipelineEnabled {
		go bundle.ensureWatcher(enrichCtx, initialLogger).Run(enrichCtx)
		go bundle.retrier(initialLogger).Run(enrichCtx)
	}

	pipeline := maybeStartWrapperlessPipeline(enrichCtx, bundle, p, initialLogger)

	bgCtx := context.WithoutCancel(ctx)

	go func() {
		<-ctx.Done()
		p.GracefulStop()
		if !wrapperlessPipelineEnabled {
			bundle.finalEnrichmentSweep(bgCtx, initialLogger)
		}
		if pipeline != nil {
			pipeline.Close()
		}
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

	// watcherInstance is the single live enrichment.Watcher. Run drives the
	// background ticker; ScanOnce / ScanOnceWithEmit cover shutdown sweep and
	// flush-session RPC. Sharing one instance keeps seen / recentEmissions /
	// tickEmittedIDs singular so the flush path cannot re-emit a sidecar the
	// background path already handled.
	watcherInstance *enrichment.Watcher
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
	client, err := analytics.NewClient(configcommon.XcodeAnalyticsURL(envProvider), tokenSupplier, logger)
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

// ensureWatcher lazily builds the single shared *enrichment.Watcher. Idempotent
// so tests seeding b.watcherInstance directly stay in control.
func (b *analyticsBundle) ensureWatcher(ctx context.Context, logger log.Logger) *enrichment.Watcher {
	if b.watcherInstance != nil {
		return b.watcherInstance
	}

	enricher := &enrichment.Enricher{
		Store:            b.pending,
		Client:           b.client,
		Auth:             b.authProvider.Get(ctx),
		Metadata:         b.metadata,
		XcodeVersion:     b.xcodeVersion,
		XcodeBuildNumber: b.xcodeBuildNumber,
		Logger:           logger,
	}
	if b.healthPath != "" {
		enricher.Health = &enrichment.HealthWriter{Path: b.healthPath}
	}
	enricher.XcresultParser = xcresult.NewDefaultParser(logger)

	var sidecarReader enrichment.SidecarReader
	if b.sessionsDir != "" {
		sidecarReader = enrichment.NewSidecarIndex(b.sessionsDir, logger)
		enricher.SidecarReader = sidecarReader
	}
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

	w := &enrichment.Watcher{
		HomeDir: b.homeDir,
		Globs: []string{
			enrichment.DefaultDerivedDataGlob,
			paths.XcodeManagedDerivedDataManifestGlobRelative,
		},
		Handle:                enricher.Enrich,
		Logger:                logger,
		MatchProbe:            matchProbe,
		MaxCorrelationRetries: enrichment.DefaultMaxCorrelationRetries,
		HandledStore:          b.handledManifests,
		SidecarReader:         sidecarReader,
		SetMetricsLookup:      enricher.SetMetricsLookup,
	}
	enricher.OnEmit = w.RecordEmittedInvocationID

	b.watcherInstance = w

	return w
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

	b.ensureWatcher(ctx, logger).ScanOnce()
	b.retrier(logger).Sweep()
}

// flushEnrichmentSweep runs a synchronous one-shot scan on the shared Watcher
// and returns the invocation IDs emitted during this sweep.
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

	b.ensureWatcher(ctx, logger).ScanOnceWithEmit(collect)
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

	return proxy.SidecarOptions{
		Dir:                  dir,
		Listener:             wrapped,
		BuildRequestResolver: proxy.NewDefaultBuildRequestResolver(logger),
	}, wrapped
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

// maybeStartWrapperlessPipeline boots the opt-in wrapperless Pipeline when the
// --wrapperless-pipeline-enabled flag is set. Returns nil when disabled or when
// the bundle is incomplete. Runs alongside the existing enricher; BE accepts
// same-ID re-PUT so overlap is harmless.
func maybeStartWrapperlessPipeline(
	ctx context.Context,
	bundle *analyticsBundle,
	p *proxy.Proxy,
	logger log.Logger,
) *wrapperless.Pipeline {
	if !wrapperlessPipelineEnabled {
		return nil
	}
	if bundle == nil || bundle.client == nil || bundle.homeDir == "" {
		logger.Debugf("Wrapperless pipeline requested but bundle incomplete; skipping")

		return nil
	}

	resolver, err := paths.Default()
	if err != nil {
		logger.Warnf("Wrapperless pipeline disabled, cannot resolve paths: %s", err)

		return nil
	}

	emitter := &wrapperless.Emitter{
		Client:              bundle.client,
		Auth:                bundle.authProvider.Get(ctx),
		Metadata:            bundle.metadata,
		XcodeVersion:        bundle.xcodeVersion,
		XcodeBuildNumber:    bundle.xcodeBuildNumber,
		Logger:              logger,
		PendingSlimPath:     resolver.WrapperlessPendingSlimPath(),
		PendingEnrichedPath: resolver.WrapperlessPendingEnrichedPath(),
		// Minted here because the `ensureWatcher` gate — which normally mints
		// it on the enricher path — is off under wrapperless.
		LocalLogAppender: invocationlog.Default(logger),
	}
	if bundle.healthPath != "" {
		// Reuse the enricher's health.json so `doctor` observes the same health
		// rhythm regardless of which flow (wrapper, enricher, wrapperless)
		// emitted the row.
		emitter.Health = &enrichment.HealthWriter{Path: bundle.healthPath}
	}

	seenStore := &wrapperless.HandledManifestStore{Path: resolver.WrapperlessHandledManifestsPath()}

	endWatcher := &wrapperless.EndWatcher{ //nolint:exhaustruct
		HomeDir:   bundle.homeDir,
		Logger:    logger,
		SeenStore: seenStore,
	}

	startWatcher := &wrapperless.StartWatcher{ //nolint:exhaustruct
		HomeDir:   bundle.homeDir,
		Logger:    logger,
		SeenStore: seenStore,
	}

	pipeline := &wrapperless.Pipeline{
		HomeDir:      bundle.homeDir,
		Logger:       logger,
		Emitter:      emitter,
		Proxy:        p,
		BlobStats:    p.BlobStatsSource(),
		StartWatcher: startWatcher,
		EndWatcher:   endWatcher,
	}

	if err := pipeline.Start(ctx); err != nil {
		logger.Warnf("Wrapperless pipeline start failed: %s", err)

		return nil
	}

	p.SetWrapperlessPipeline(pipeline)

	logger.Infof("Wrapperless pipeline running alongside enricher (opt-in).")

	return pipeline
}
