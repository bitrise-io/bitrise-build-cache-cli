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
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/proxypid"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeversion"
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
					log.WithDebugLog(config.DebugLogging || common.IsDebugLogMode),
					log.WithOutput(io.MultiWriter(os.Stdout, f)),
				)

				return logger, nil
			}

			initialLogger, err := loggerFactory(initialInvocationID)
			if err != nil {
				return fmt.Errorf("failed to create initialLogger: %w", err)
			}

			initialLogger.TInfof("Xcelerate Proxy")

			release, err := proxypid.Acquire(osProxy, xcelerate.PathFor(osProxy, paths.ProxyPidFileName), nil)
			if err != nil {
				initialLogger.Infof("Skipping proxy startup: %s", err)

				return nil
			}
			defer func() {
				if err := release(); err != nil {
					initialLogger.Warnf("Failed to release proxy pid lock: %s", err)
				}
			}()

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
	oauthCfg := oauth.NewConfigFromEnv(envProvider)
	oauthCfg.Logger = initialLogger
	refreshFn := func(ctx context.Context) (string, string, error) {
		creds, err := oauthCfg.EnsureFresh(ctx)
		if err != nil {
			return "", "", fmt.Errorf("ensure fresh oauth credentials: %w", err)
		}

		return creds.PAT, creds.WorkspaceID, nil
	}
	authProvider := configcommon.NewExpiryAwareResolver(ctx, envProvider, refreshFn, initialLogger)

	client, err := common.CreateKVClient(ctx, common.CreateKVClientParams{
		CacheOperationID:   uuid.New().String(),
		ClientName:         common.ClientNameXcode,
		AuthConfig:         config.AuthConfig,
		AuthSource:         authProvider,
		Envs:               envProvider,
		CommandFunc:        commandFunc,
		Logger:             initialLogger,
		BitriseKVClient:    bitriseKVClient,
		EndpointURL:        config.BuildCacheEndpoint,
		CapabilitiesClient: capabilitiesClient,
		InvocationID:       initialInvocationID,
		SkipCapabilities:   true, // proxy handles capabilities calls internally
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

	p := proxy.NewProxy(client, config.PushEnabled, initialLogger, loggerFactory, emitter)
	p.InactivityTimeout = resolveInactivityTimeout(envProvider, initialLogger)

	if bundle.enrichmentEnabled() {
		go bundle.watcher(initialLogger).Run(ctx)
		go bundle.retrier(initialLogger).Run(ctx)
	}

	go func() {
		<-ctx.Done()
		p.GracefulStop()
	}()

	serveErr := p.Serve(listener)

	p.FlushCurrentSession(context.WithoutCancel(ctx))

	//nolint:wrapcheck
	return serveErr
}

type analyticsBundle struct {
	client           *analytics.Client
	authProvider     *configcommon.ExpiryAwareResolver
	metadata         configcommon.CacheConfigMetadata
	pending          *enrichment.Store
	handledManifests *enrichment.HandledManifestStore
	healthPath       string
	homeDir          string
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
	authProvider *configcommon.ExpiryAwareResolver,
) *analyticsBundle {
	tokenSupplier := func() string { return authProvider.Get().TokenInGradleFormat() }
	client, err := analytics.NewClient(consts.XcodeAnalyticsServiceEndpoint, tokenSupplier, logger)
	if err != nil {
		logger.Warnf("Xcode analytics disabled — client init failed: %s", err)

		return &analyticsBundle{logger: logger}
	}

	b := &analyticsBundle{
		client:       client,
		authProvider: authProvider,
		metadata:     configcommon.NewMetadata(envProvider, commandFunc, logger),
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

func (b *analyticsBundle) watcher(logger log.Logger) *enrichment.Watcher {
	enricher := &enrichment.Enricher{
		Store:            b.pending,
		Client:           b.client,
		Auth:             b.authProvider.Get(),
		Metadata:         b.metadata,
		XcodeVersion:     b.xcodeVersion,
		XcodeBuildNumber: b.xcodeBuildNumber,
		Logger:           logger,
	}
	if b.healthPath != "" {
		enricher.Health = &enrichment.HealthWriter{Path: b.healthPath}
	}

	matchProbe := func(entry enrichment.ManifestEntry) bool {
		if b.pending == nil {
			return false
		}

		records, err := b.pending.Load()
		if err != nil {
			return false
		}

		_, matched := enrichment.Correlate(entry, records)

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
		HandledStore:          b.handledManifests,
	}
}

func (b *analyticsBundle) retrier(logger log.Logger) *enrichment.Retrier {
	return &enrichment.Retrier{
		Store:  b.pending,
		Client: b.client,
		Logger: logger,
	}
}

type slimInvocationEmitter struct {
	bundle *analyticsBundle
}

func (e *slimInvocationEmitter) EmitSlim(ctx context.Context, meta proxy.SessionMeta, stats proxy.SessionStats) {
	b := e.bundle

	endTime := meta.EndTime
	if endTime.IsZero() {
		endTime = time.Now()
	}
	duration := endTime.Sub(meta.StartTime).Milliseconds()
	hitRate := stats.HitRate()

	// Pending has to survive the marker check so F2's manifest scan can correlate the wrapper build back to this InvocationID — otherwise F2 mints a duplicate orphan.
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

	if enrichment.MarkerExists(meta.InvocationID) {
		b.logger.Debugf("Slim emit skipped for %s: wrapper already handled", meta.InvocationID)

		return
	}

	go func() {
		inv := analytics.NewInvocation(analytics.InvocationRunStats{
			InvocationDate: meta.StartTime,
			InvocationID:   meta.InvocationID,
			Duration:       duration,
			HitRate:        hitRate,
		}, b.authProvider.Get(), b.metadata)

		if err := b.client.PutInvocation(*inv); err != nil {
			b.logger.Warnf("Failed to emit slim invocation %s: %s", meta.InvocationID, err)

			return
		}

		b.logger.Debugf("Slim invocation emitted: %s (hit-rate %.02f%%)", meta.InvocationID, hitRate*100)
	}()

	_ = ctx
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
