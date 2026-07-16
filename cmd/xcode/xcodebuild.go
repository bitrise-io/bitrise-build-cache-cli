package xcode

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retry"
	"github.com/dustin/go-humanize"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/analytics/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/common/childstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
)

const (
	startedProxy = "Started xcelerate_proxy pid = %d"

	NoBitriseBuildCacheFlag     = "--no-bitrise-build-cache"
	NoPrefixMapFlag             = "--no-prefix-map"
	NoManagedDerivedDataFlag    = "--no-managed-derived-data"
	CreateXCFrameworkFlag       = "-create-xcframework"
	MsgBuildCacheDisabledByFlag = "Build cache disabled by %s flag"
	MsgArgsPassedToXcodebuild   = "Arguments passed to xcodebuild: %v"
	MsgInvocationSuccess        = "Invocation succeeded ✅ after %s"
	MsgInvocationFailed         = "Invocation failed ❌ after %s: %s"
	MsgInvocationSaved          = "Invocation saved. Visit 👉 https://app.bitrise.io/build-cache/invocations/xcode/%s"

	ErrExecutingXcode = "Error executing xcodebuild: %v"
	ErrReadConfig     = "Error reading config: %v"

	errFmtExecutable         = "executable: %w"
	errFmtFailedToStartProxy = "failed to start proxy: %w"
	errFmtFailedToCreatePID  = "failed to create pid file: %w"
)

//go:generate moq -rm -stub -pkg mocks -out ./mocks/session_client.go ./../../proto/llvm/session SessionClient

//go:generate moq -out mocks/runner_mock.go -pkg mocks . XcodeRunner
type XcodeRunner interface {
	Run(ctx context.Context, args []string) xcodeargs.RunStats
}

type XcelerateParams struct {
	OrigArgs []string
}

//nolint:gochecknoglobals
var xcelerateParams = XcelerateParams{
	OrigArgs: []string{},
}

// rootCmd represents the base command when called without any subcommands
//
//nolint:gochecknoglobals
var xcodebuildCmd = &cobra.Command{
	Use:   "xcodebuild",
	Short: "TBD",
	Long: `xcodebuild -  Wrapper around xcodebuild to enable Bitrise Build Cache.
TBD`,
	SilenceUsage:       true,
	DisableFlagParsing: true, // pass all args to xcodebuild
	RunE: func(cobraCmd *cobra.Command, _ []string) error {
		invocationID := uuid.New().String()

		if parentID := os.Getenv("BITRISE_INVOCATION_ID"); parentID != "" {
			fmt.Fprintf(os.Stderr, "Xcode invocation ID: %s (parent: %s)\n", invocationID, parentID)
		} else {
			fmt.Fprintf(os.Stderr, "Xcode invocation ID: %s (no parent)\n", invocationID)
		}

		decoder := utils.DefaultDecoderFactory{}
		osProxy := utils.DefaultOsProxy{}
		config, err := xcelerate.ReadConfig(osProxy, decoder)
		if err != nil {
			// we don't have the config yet, use default logger
			log.NewLogger().Errorf(ErrReadConfig, err)
			config = xcelerate.DefaultConfig()
		}

		xcelerateParams.OrigArgs = os.Args[1:]

		silentLogging := config.Silent
		if slices.Contains(xcelerateParams.OrigArgs, "-json") {
			silentLogging = true
		}

		// Strip wrapper-only flags from argv and capture disable-reasons before
		// the logger exists — they get logged once the logger is wired below.
		var disabledBy []string
		if slices.Contains(xcelerateParams.OrigArgs, NoBitriseBuildCacheFlag) {
			xcelerateParams.OrigArgs = slices.DeleteFunc(xcelerateParams.OrigArgs, func(s string) bool {
				return s == NoBitriseBuildCacheFlag
			})
			config.BuildCacheEnabled = false
			disabledBy = append(disabledBy, NoBitriseBuildCacheFlag)
		}

		noPrefixMap := slices.Contains(xcelerateParams.OrigArgs, NoPrefixMapFlag)
		if noPrefixMap {
			xcelerateParams.OrigArgs = slices.DeleteFunc(xcelerateParams.OrigArgs, func(s string) bool {
				return s == NoPrefixMapFlag
			})
		}

		noManagedDD := slices.Contains(xcelerateParams.OrigArgs, NoManagedDerivedDataFlag)
		if noManagedDD {
			xcelerateParams.OrigArgs = slices.DeleteFunc(xcelerateParams.OrigArgs, func(s string) bool {
				return s == NoManagedDerivedDataFlag
			})
		}

		// Automatically disable cache for -create-xcframework as it's incompatible
		if slices.Contains(xcelerateParams.OrigArgs, CreateXCFrameworkFlag) {
			config.BuildCacheEnabled = false
			disabledBy = append(disabledBy, CreateXCFrameworkFlag)
		}

		// Preview xcodeArgs with a nil logger so we can decide whether this is a
		// query invocation (no build action). Query invocations short-circuit
		// before creating the per-invocation log file or spawning the proxy.
		previewXcodeArgs := xcodeargs.NewDefault(cobraCmd, xcelerateParams.OrigArgs, log.NewLogger())
		isBuildAction := previewXcodeArgs.HasBuildAction()

		var (
			logFileWC io.WriteCloser
			logPath   string
		)
		if isBuildAction {
			envs := utils.AllEnvs()
			var logErr error
			logFileWC, logPath, logErr = logFile(invocationID, osProxy, envs)
			if logErr != nil && !config.Silent {
				fmt.Fprintf(os.Stderr, "Failed to create log file: %v\n", logErr)
			}
			defer func() {
				if logFileWC != nil {
					_ = logFileWC.Close()
				}
			}()
		}

		logOutput := wrapperLogWriter(logFileWC, logPath, silentLogging)
		logger := log.NewLogger(log.WithPrefix("[Bitrise Analytics] "), log.WithOutput(logOutput))
		cacheLogger := log.NewLogger(log.WithPrefix("[Bitrise Build Cache] "), log.WithOutput(logOutput))

		if !silentLogging {
			for _, flag := range disabledBy {
				logger.TInfof(MsgBuildCacheDisabledByFlag, flag)
			}
		}

		xcodeArgs := xcodeargs.NewDefault(
			cobraCmd,
			xcelerateParams.OrigArgs,
			logger,
		)

		logger.EnableDebugLog(config.DebugLogging)

		var proxySessionClient session.SessionClient
		if isBuildAction && config.BuildCacheEnabled {
			logger.TInfof("Cache enabled, starting xcelerate proxy connecting to: %s", config.BuildCacheEndpoint)

			err := startProxy(
				logger,
				osProxy,
				utils.DefaultCommandFunc(),
				func(pid int, signum syscall.Signal) {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				},
				config.ProxySocketPath,
			)
			if err != nil {
				return fmt.Errorf(errFmtFailedToStartProxy, err)
			}

			var cleanup func()

			proxySessionClient, cleanup = createProxySessionClient(config, logger)
			defer cleanup()
		}

		metadata := common.NewMetadata(utils.AllEnvs(), func(cmd string, args ...string) (string, error) {
			o, err := utils.DefaultCommandFunc()(cobraCmd.Context(), cmd, args...).CombinedOutput()

			return string(o), err
		}, logger)

		xcodeRunner := xcodeargs.NewRunner(logger, config, logFileWC)

		runner := &XcodebuildRunner{
			Config:             config,
			Metadata:           metadata,
			InvocationID:       invocationID,
			Logger:             logger,
			CacheLogger:        cacheLogger,
			XcodeRunner:        xcodeRunner,
			ProxySessionClient: proxySessionClient,
			XcodeArgs:          xcodeArgs,
			NoPrefixMap:        noPrefixMap,
			NoManagedDD:        noManagedDD,
		}
		if runStats := runner.Run(cobraCmd.Context()); runStats.Error != nil {
			logger.Errorf(ErrExecutingXcode, runStats.Error)
			os.Exit(runStats.ExitCode)
		}

		return nil
	},
}

func init() {
	xcelerateCommand.AddCommand(xcodebuildCmd)
}

func logFile(invocationID string, osProxy utils.OsProxy, envs map[string]string) (io.WriteCloser, string, error) {
	deployDir := envs["BITRISE_DEPLOY_DIR"]
	var logDir string
	if deployDir != "" {
		logDir = deployDir
	} else {
		var err error
		logDir, err = getLogDir(osProxy)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get log dir: %w", err)
		}
	}

	logPath := fmt.Sprintf("%s/xcelerate-%s.log", logDir, invocationID)
	f, err := osProxy.Create(logPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create log file: %w", err)
	}

	return f, logPath, nil
}

func wrapperLogWriter(logFile io.Writer, logFilePath string, silent bool) io.Writer {
	if logFile == nil {
		if silent {
			return io.Discard
		}
		fmt.Fprintf(os.Stderr, "Using stderr only...\n")

		return os.Stderr
	}
	// wrap stderr to also write to log file
	var w io.Writer
	if silent {
		w = logFile
	} else {
		//nolint:errcheck
		fmt.Fprintf(os.Stderr, "ℹ️ These logs are available at: %s\n", logFilePath)

		w = io.MultiWriter(os.Stderr, logFile)
	}

	return w
}

//go:generate moq -stub -out xcodebuild_relation_mock_test.go -pkg xcode . relationSender

//go:generate moq -stub -out xcodebuild_local_log_mock_test.go -pkg xcode . localInvocationLogger

type invocationSaver interface {
	PutInvocation(inv analytics.Invocation) error
}

type relationSender interface {
	PutInvocationRelation(rel multiplatform.InvocationRelation) error
}

type localInvocationLogger interface {
	Append(rec invocations.Record) error
}

// XcodebuildRunner holds configuration for the xcodebuild wrapper and provides
// Run as its main entry point.
type XcodebuildRunner struct {
	Config             xcelerate.Config
	Metadata           common.CacheConfigMetadata
	InvocationID       string
	Logger             log.Logger
	CacheLogger        log.Logger
	XcodeRunner        XcodeRunner
	ProxySessionClient session.SessionClient
	XcodeArgs          xcodeargs.XcodeArgs

	// NoPrefixMap suppresses prefix-map injection for this invocation only
	// (per-invocation counterpart to Config.DisablePrefixMapping).
	NoPrefixMap bool
	// NoManagedDD suppresses the wrapper-owned -derivedDataPath and PROJECT_TEMP_DIR
	// substitution; user-supplied values are still honoured either way.
	NoManagedDD bool

	// Paths is the on-disk root resolver. If zero, paths.Default() is used.
	Paths paths.Paths

	// invocationAPI saves invocations. If nil, a production analytics client is created.
	invocationAPI invocationSaver
	// relationAPI sends invocation relations. If nil, a production multiplatform client is created.
	relationAPI relationSender
	// localLogger appends to the local invocation log. If nil, paths.Default + invocations.NewWriter is used.
	localLogger localInvocationLogger
}

// Run executes the xcodebuild wrapper: runs xcodebuild, collects stats,
// sends analytics, and registers invocation relations.
//
//nolint:nestif
func (c *XcodebuildRunner) Run(ctx context.Context) xcodeargs.RunStats {
	toPass := c.assembleArgs()
	c.Logger.TDebugf(MsgArgsPassedToXcodebuild, toPass)

	// Query invocations have no session state or cache stats worth reporting;
	// skip SetSession + analytics emit + marker write entirely.
	if !c.XcodeArgs.HasBuildAction() {
		return c.runPassthrough(ctx, toPass)
	}

	if c.ProxySessionClient != nil {
		_, err := c.ProxySessionClient.SetSession(ctx, &session.SetSessionRequest{
			InvocationId: c.InvocationID,
			AppSlug:      c.Metadata.BitriseAppID,
			BuildSlug:    c.Metadata.BitriseBuildID,
			StepSlug:     c.Metadata.BitriseStepExecutionID,
		})
		if err != nil {
			c.Logger.TErrorf("Failed to set session: %v", err)
		}

		if !c.Config.Silent {
			c.Logger.Debugf("Streaming proxy logs...")
			proxyCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			go func() {
				if err := streamProxyLogs(proxyCtx, c.InvocationID, c.CacheLogger, utils.DefaultOsProxy{}); err != nil {
					c.Logger.Errorf("Failed to stream proxy logs: %v", err)
				} else {
					c.Logger.Debugf("Proxy logs stream closed")
				}
			}()
		}
	}

	runStats := c.XcodeRunner.Run(ctx, toPass)
	if runStats.Error != nil {
		c.Logger.TErrorf(MsgInvocationFailed, time.Duration(runStats.DurationMS)*time.Millisecond, runStats.Error)
	} else {
		c.Logger.TDonef(MsgInvocationSuccess, time.Duration(runStats.DurationMS)*time.Millisecond)
	}
	c.Logger.Debugf("Run stats: %+v", runStats)

	hitRate := getHitRateFromSessionAndRunStats(ctx, c.ProxySessionClient, runStats, c.Logger)

	if !shouldSaveInvocation(c.XcodeArgs) {
		c.Logger.TDebugf("Save invocation skipped")

		return runStats
	}

	c.Metadata.BenchmarkPhase = resolveBenchmarkPhase(c.Logger)

	inv := analytics.NewInvocation(analytics.InvocationRunStats{
		InvocationDate:   runStats.StartTime,
		InvocationID:     c.InvocationID,
		Duration:         runStats.DurationMS,
		HitRate:          hitRate,
		Command:          c.XcodeArgs.ShortCommand(),
		FullCommand:      c.XcodeArgs.Command(),
		Success:          runStats.Success,
		Error:            runStats.Error,
		XcodeVersion:     runStats.XcodeVersion,
		XcodeBuildNumber: runStats.XcodeBuildNumber,
	}, c.Config.AuthConfig, c.Metadata)

	c.appendLocalInvocationLog(*inv, runStats)
	c.saveInvocationAndRelation(*inv, runStats.CacheStats.Hits, runStats.CacheStats.TotalTasks)

	return runStats
}

// runPassthrough forwards a query xcodebuild invocation without touching the
// proxy or emitting analytics.
func (c *XcodebuildRunner) runPassthrough(ctx context.Context, args []string) xcodeargs.RunStats {
	c.Logger.TDebugf("xcodebuild wrapper passthrough for query invocation")

	runStats := c.XcodeRunner.Run(ctx, args)
	if runStats.Error != nil {
		c.Logger.TErrorf(MsgInvocationFailed, time.Duration(runStats.DurationMS)*time.Millisecond, runStats.Error)
	} else {
		c.Logger.TDonef(MsgInvocationSuccess, time.Duration(runStats.DurationMS)*time.Millisecond)
	}

	return runStats
}

func (c *XcodebuildRunner) appendLocalInvocationLog(inv analytics.Invocation, runStats xcodeargs.RunStats) {
	logger := c.resolveLocalLogger()
	if logger == nil {
		return
	}

	src := invocations.SourceLocal
	if c.Metadata.CIProvider != "" {
		src = invocations.SourceCI
	}

	startedAt := runStats.StartTime
	finishedAt := startedAt.Add(time.Duration(runStats.DurationMS) * time.Millisecond)

	rec := invocations.Record{
		InvocationID: inv.InvocationID,
		Command:      inv.Command,
		Tool:         invocations.ToolXcode,
		ToolVersion:  inv.XcodeVersion,
		CLIVersion:   inv.CLIVersion,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		ExitCode:     runStats.ExitCode,
		Source:       src,
	}

	if err := logger.Append(rec); err != nil {
		c.Logger.Warnf("Failed to append local invocation log: %v", err)
	}
}

func (c *XcodebuildRunner) resolveLocalLogger() localInvocationLogger {
	if c.localLogger != nil {
		return c.localLogger
	}

	p, err := paths.Default()
	if err != nil {
		c.Logger.Warnf("Skipping local invocation log: %v", err)

		return nil
	}

	w := invocations.NewWriter(p)
	w.Logger = c.Logger

	return w
}

func (c *XcodebuildRunner) saveInvocationAndRelation(inv analytics.Invocation, hits, total int64) {
	saver, err := c.resolveInvocationAPI()
	if err != nil {
		c.Logger.Errorf("Failed to create analytics client: %v", err)

		return
	}

	if err := saver.PutInvocation(inv); err != nil {
		c.Logger.Errorf("Failed to send invocation analytics: %v", err)

		return
	}

	enrichment.WriteMarker(c.Logger, c.InvocationID)

	c.Logger.TInfof(MsgInvocationSaved, c.InvocationID)

	if parentID := os.Getenv("BITRISE_INVOCATION_ID"); parentID != "" {
		c.sendRelation(parentID)
		c.writeChildStatsLedger(parentID, inv, hits, total)
	}
}

// writeChildStatsLedger records this xcode invocation's hit rate in the
// parent's local ledger so a parent wrapper (e.g. react-native) can
// aggregate child hit rates at the end of its run.
//
// hits and total come from xcode's reported per-target cache stats so the
// parent can compute a weighted hit rate (sum(hits)/sum(total)) in addition
// to the simple mean of per-child hit rates.
func (c *XcodebuildRunner) writeChildStatsLedger(parentID string, inv analytics.Invocation, hits, total int64) {
	entry := childstats.Entry{
		ChildInvocationID:  c.InvocationID,
		ParentInvocationID: parentID,
		BuildTool:          "xcode",
		HitRate:            inv.HitRate,
		Hits:               hits,
		Total:              total,
		BenchmarkPhase:     c.Metadata.BenchmarkPhase,
		Failed:             !inv.Success,
	}

	if err := childstats.NewWriter().Write(entry); err != nil {
		c.Logger.Warnf("Failed to write child stats ledger: %v", err)
	}
}

func (c *XcodebuildRunner) resolveInvocationAPI() (invocationSaver, error) {
	if c.invocationAPI != nil {
		return c.invocationAPI, nil
	}

	client, err := analytics.NewClient(consts.XcodeAnalyticsServiceEndpoint, c.Config.AuthConfig.TokenInGradleFormat(), c.Logger)
	if err != nil {
		return nil, fmt.Errorf("create analytics client: %w", err)
	}

	return client, nil
}

func (c *XcodebuildRunner) resolveRelationAPI() (relationSender, error) {
	if c.relationAPI != nil {
		return c.relationAPI, nil
	}

	client, err := multiplatform.NewClient(consts.MultiplatformAnalyticsServiceEndpoint, c.Config.AuthConfig.TokenInGradleFormat(), c.Logger)
	if err != nil {
		return nil, fmt.Errorf("create multiplatform analytics client: %w", err)
	}

	return client, nil
}

func (c *XcodebuildRunner) sendRelation(parentID string) {
	c.Logger.TInfof("Registering invocation relation: parent=%s → child=%s (build-tool=xcode)", parentID, c.InvocationID)

	api, err := c.resolveRelationAPI()
	if err != nil {
		c.Logger.Errorf("Failed to create multiplatform analytics client: %v", err)

		return
	}

	rel := multiplatform.InvocationRelation{
		ParentInvocationID: parentID,
		ChildInvocationID:  c.InvocationID,
		InvocationDate:     time.Now(),
		BuildTool:          "xcode",
	}

	if err := api.PutInvocationRelation(rel); err != nil {
		c.Logger.Errorf("Failed to send invocation relation analytics: %v", err)
	}
}

//nolint:nestif
func getHitRateFromSessionAndRunStats(ctx context.Context,
	proxySessionClient session.SessionClient,
	runStats xcodeargs.RunStats,
	logger log.Logger,
) float32 {
	var hitRate float32
	// If build cache is not enabled, session client is nil
	if proxySessionClient != nil {
		proxyStats, err := proxySessionClient.GetSessionStats(ctx, &empty.Empty{})

		if err != nil || proxyStats == nil {
			logger.Warnf("Failed to get proxy session stats: %v", err)
		} else {
			// Lowest prio: blob-based hit rate
			if proxyStats.GetHits()+proxyStats.GetMisses() > 0 {
				hitRate = float32(proxyStats.GetHits()) / float32(proxyStats.GetHits()+proxyStats.GetMisses())
			}
			logger.Infof(
				"Proxy blob stats: hits: %d (%s) / total: %d (%.02f%%). Uploaded blobs: %d (%s)",
				proxyStats.GetHits(),
				humanize.Bytes(uint64(proxyStats.GetDownloadedBytes())), // nolint: gosec
				proxyStats.GetHits()+proxyStats.GetMisses(),
				hitRate*100,
				proxyStats.GetUploads(),
				humanize.Bytes(uint64(proxyStats.GetUploadedBytes())), // nolint: gosec
			)

			// If we have KV stats, use that instead of blob stats.
			if proxyStats.GetKvHits()+proxyStats.GetKvMisses() > 0 {
				hitRate = float32(proxyStats.GetKvHits()) / float32(proxyStats.GetKvHits()+proxyStats.GetKvMisses())
				logger.Infof(
					"Proxy KV stats: hits: %d / total: %d (%.02f%%). Uploaded KV blobs: %s",
					proxyStats.GetKvHits(),
					proxyStats.GetKvHits()+proxyStats.GetKvMisses(),
					hitRate*100,
					humanize.Bytes(uint64(proxyStats.GetKvUploadedBytes())), // nolint: gosec
				)
			}
		}
	}

	// Calculate log-based task cache hit rate even if build cache is not enabled.
	// It shall also take priority over the proxy stats.
	if runStats.CacheStats.TotalTasks > 0 {
		hitRate = float32(runStats.CacheStats.Hits) / float32(runStats.CacheStats.TotalTasks)
		logger.Infof(
			"Xcode task stats: hits: %d / total: %d (%.02f%%)",
			runStats.CacheStats.Hits,
			runStats.CacheStats.TotalTasks,
			hitRate*100,
		)
	}

	return hitRate
}

// resolveBenchmarkPhase reads the benchmark phase from:
// 1. BITRISE_BUILD_CACHE_BENCHMARK_PHASE_XCODE env var (set during activation)
// 2. ~/.local/state/xcelerate/benchmark/benchmark-phase-xcode.json (file fallback)
func resolveBenchmarkPhase(logger log.Logger) string {
	if phase := os.Getenv(common.BenchmarkPhaseEnvVar(common.BuildToolXcode)); phase != "" {
		logger.Debugf("Benchmark phase from env: %s", phase)

		return phase
	}

	if phase := common.ReadBenchmarkPhaseFile(common.BuildToolXcode, logger); phase != "" {
		logger.Debugf("Benchmark phase from file: %s", phase)

		return phase
	}

	return ""
}

func shouldSaveInvocation(xcodeArgs xcodeargs.XcodeArgs) bool {
	// nolint: staticcheck
	if xcodeArgs.Command() == "-version" {
		return false
	}

	return true
}

// createProxySessionClient creates a gRPC client to connect to the proxy session service. If any error occurs during the
// connection, it returns nil.
func createProxySessionClient(config xcelerate.Config, logger log.Logger) (session.SessionClient, func()) {
	proxySocket := "unix://" + strings.TrimPrefix(config.ProxySocketPath, "unix://")

	logger.TInfof("Connecting to proxy socket: %s", proxySocket)

	clientConn, err := grpc.NewClient(proxySocket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.TErrorf("Failed to create gRPC client: %v", err)

		return nil, func() {}
	}

	return session.NewSessionClient(clientConn), func() {
		if err := clientConn.Close(); err != nil {
			logger.TErrorf("Failed to close gRPC client connection: %v", err)
		}
	}
}

// assembleArgs returns the final argv for xcodebuild: user args plus the
// wrapper-owned build settings and prefix-map splicing when build cache is on.
func (c *XcodebuildRunner) assembleArgs() []string {
	additional := map[string]string{}

	if !c.Config.BuildCacheEnabled {
		return c.XcodeArgs.Args(additional)
	}

	// Query-only invocations (-list, -version, -showBuildSettings on its own, ...)
	// reject -derivedDataPath and don't need cache wiring. Run() short-circuits
	// these into runPassthrough before assembleArgs; this branch is defensive
	// only — return argv unchanged with no cache-flag injection.
	if !c.XcodeArgs.HasBuildAction() {
		return c.XcodeArgs.Args(additional)
	}

	additional["COMPILATION_CACHE_REMOTE_SERVICE_PATH"] = c.Config.ProxySocketPath
	if c.Config.BuildCacheSkipFlags {
		return c.XcodeArgs.Args(additional)
	}

	maps.Copy(additional, xcodeargs.CacheArgs)

	diagnosticRemarks := "NO"
	if c.Config.DebugLogging {
		diagnosticRemarks = "YES"
	}
	additional["COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS"] = diagnosticRemarks

	var extraArgv []string
	var userOtherCFlagsToSplice string
	var mergedOtherCFlags string

	if !c.Config.DisablePrefixMapping && !c.NoPrefixMap {
		ps := c.resolvePrefixMapPaths()
		suffix := xcodeargs.BuildOtherCFlagsValue(ps)

		additional[xcodeargs.ClangEnablePrefixMappingKey] = "YES"

		if ps.ProjectTempDir != "" {
			additional[xcodeargs.ProjectTempDirKey] = ps.ProjectTempDir
		}

		userOtherCFlagsToSplice = c.XcodeArgs.UserOtherCFlags()
		mergedOtherCFlags = xcodeargs.MergeOtherCFlagsValue(userOtherCFlagsToSplice, suffix)

		if ps.DerivedDataPath != "" && c.XcodeArgs.DerivedDataPath() == "" {
			extraArgv = append(extraArgv, xcodeargs.DerivedDataPathFlag, ps.DerivedDataPath)
		}
	}

	toPass := c.XcodeArgs.Args(additional)

	if mergedOtherCFlags != "" {
		toPass = replaceOrAppendBuildSetting(toPass, xcodeargs.OtherCFlagsKey, mergedOtherCFlags)
		if userOtherCFlagsToSplice != "" && c.Metadata.CIProvider == "" {
			c.Logger.TWarnf("Merged user OTHER_CFLAGS with Bitrise prefix-map rules; pass --no-prefix-map to opt out.")
		}
	}

	return append(toPass, extraArgv...)
}

// resolvePrefixMapPaths determines the four absolute paths that feed the
// narrowest-first prefix-map rules. User-supplied DerivedDataPath /
// PROJECT_TEMP_DIR take precedence; otherwise the wrapper-owned dirs under
// ~/.bitrise/cache/xcode-{dd,ptd}/<sha> are used unless c.NoManagedDD is set.
func (c *XcodebuildRunner) resolvePrefixMapPaths() xcodeargs.PrefixMapPaths {
	projectDir := c.XcodeArgs.ProjectDir()
	home, _ := os.UserHomeDir()

	dd := c.XcodeArgs.DerivedDataPath()
	ptd := c.XcodeArgs.ProjectTempDir()

	if !c.NoManagedDD && projectDir != "" {
		sha := workspaceSHA(projectDir)
		p := c.resolvePaths()
		if dd == "" {
			dd = p.XcodeManagedDerivedDataDir(sha)
		}
		if ptd == "" {
			ptd = p.XcodeManagedProjectTempDir(sha)
		}
	}

	return xcodeargs.PrefixMapPaths{
		Home:            home,
		ProjectDir:      projectDir,
		DerivedDataPath: dd,
		ProjectTempDir:  ptd,
	}
}

func (c *XcodebuildRunner) resolvePaths() paths.Paths {
	if c.Paths.Home != "" {
		return c.Paths
	}
	p, err := paths.Default()
	if err != nil {
		c.Logger.Warnf("Could not resolve user home for managed DerivedData: %v", err)

		return paths.Paths{}
	}

	return p
}

// workspaceSHA derives a stable per-checkout key from the workspace directory's
// absolute path — same path yields the same DD/PTD dir; different absolute paths
// stay isolated so concurrent xcodebuild runs against parallel checkouts do not
// stomp on each other's DerivedData.
func workspaceSHA(projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))

	return hex.EncodeToString(sum[:8])
}

// replaceOrAppendBuildSetting substitutes any existing "KEY=..." entry in argv
// with the supplied value, or appends it when absent. The caller is responsible
// for pre-computing the merged value.
func replaceOrAppendBuildSetting(argv []string, key, value string) []string {
	out := make([]string, 0, len(argv)+1)
	replaced := false
	for _, arg := range argv {
		if strings.HasPrefix(arg, key+"=") {
			if !replaced {
				out = append(out, key+"="+value)
				replaced = true
			}

			continue
		}
		out = append(out, arg)
	}
	if !replaced {
		out = append(out, key+"="+value)
	}

	return out
}

// isProxyReachable reports whether a unix-socket listener is answering at socketPath.
// A short DialContext timeout is enough — the wrapper only needs to know a peer is
// bound, not that any RPC round-trips. ECONNREFUSED / ENOENT / timeout all return false.
func isProxyReachable(socketPath string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return false
	}
	_ = conn.Close()

	return true
}

func startProxy(
	logger log.Logger,
	osProxy utils.OsProxy,
	commandFunc utils.CommandFunc,
	killFunc func(pid int, signum syscall.Signal),
	socketPath string,
) error {
	if socketPath != "" && isProxyReachable(socketPath) {
		logger.TDonef("Xcelerate proxy already reachable at %s — reusing", socketPath)

		return nil
	}

	pidFilePth := xcelerate.PathFor(osProxy, paths.ProxyPidFileName)

	content, exists, err := osProxy.ReadFileIfExists(pidFilePth)
	if err != nil {
		return fmt.Errorf("failed to read pid file: %w", err)
	}
	if exists {
		pid, err := strconv.Atoi(strings.TrimSpace(content))
		if err != nil {
			return fmt.Errorf("failed to parse pid file content: %w", err)
		}

		logger.TInfof("Attempting to connect to an already running proxy (pid: %d)", pid)

		process, err := osProxy.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("failed to find process: %w", err)
		}

		if err := process.Signal(syscall.Signal(0)); err == nil {
			logger.TDonef("Xcelerate proxy already running (pid: %d)", pid)

			return nil
		}

		logger.TWarnf("Removing stale pid file (pid: %d)", pid)
		if err := osProxy.Remove(pidFilePth); err != nil {
			return fmt.Errorf("failed to remove stale pid file: %w", err)
		}
	}

	exe, err := osProxy.Executable()
	if err != nil {
		return fmt.Errorf(errFmtExecutable, err)
	}

	cmd := commandFunc(context.Background(), exe, xcelerateCommand.Use, xcelerateProxyCmd.Use)

	// Detach into new process group so we can signal the whole group.
	cmd.SetSysProcAttr(&syscall.SysProcAttr{
		Setpgid: true, // create a new process group with pgid = pid
	})

	cmd.SetStdout(nil) // handled by streamProxyLogs
	cmd.SetStderr(nil)
	cmd.SetStdin(nil)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf(errFmtFailedToStartProxy, err)
	}

	pid := cmd.PID()
	if err := osProxy.WriteFile(pidFilePth, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		killFunc(pid, syscall.SIGKILL)

		return fmt.Errorf(errFmtFailedToCreatePID, err)
	}

	logger.TDonef(startedProxy, pid)

	return nil
}

func streamProxyLogs(ctx context.Context, invocationID string, logger log.Logger, osProxy utils.OsProxy) error {
	var f *os.File
	err := retry.Times(10).Wait(1 * time.Second).TryWithAbort(func(attempt uint) (error, bool) {
		proxyLogFile, err := getProxyLogFile(osProxy, invocationID)
		if err != nil {
			logger.TDebugf("Failed to get proxy log file, attempt: %d, error: %v", attempt+1, err)

			return fmt.Errorf("failed to get proxy log file: %w", err), false
		}

		f, err = osProxy.OpenFile(proxyLogFile, os.O_RDONLY, 0)
		if err != nil {
			logger.TDebugf("Failed to open proxy log file, attempt: %d, error: %v", attempt+1, err)

			return fmt.Errorf("failed to open proxy log file: %w", err), false
		}
		logger.TDebugf("Streaming proxy log file: %s", proxyLogFile)

		return nil, true
	})
	if err != nil {
		return fmt.Errorf("failed to open proxy log file after retries: %w", err)
	}

	go func() {
		<-ctx.Done()
		logger.Debugf("Context done, closing log file")
		err := f.Close()
		if err != nil {
			logger.Errorf("Failed to close log file: %v", err)
		}
	}()

	r := bufio.NewReader(f)
	for {
		select {
		case <-ctx.Done():
			logger.Debugf("Context done, exiting streamProxyLogs")

			return nil
		default:
		}
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				time.Sleep(500 * time.Millisecond)

				continue
			}

			return fmt.Errorf("failed to read proxy log line: %w", err)
		}

		logger.Printf(strings.TrimSpace(line))
	}
}
