package xcode

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/dustin/go-humanize"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/analytics/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/xcelerate/xcodeargs"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/proto/llvm/session"
)

const (
	pidFile      = "proxy.pid"
	startedProxy = "Started xcelerate_proxy pid = %d"

	NoBitriseBuildCacheFlag     = "--no-bitrise-build-cache"
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

		envs := utils.AllEnvs()
		logFile, logPath, err := logFile(invocationID, osProxy, envs)
		if err != nil && !config.Silent {
			fmt.Fprintf(os.Stderr, "Failed to create log file: %v\n", err)
		}
		defer func() {
			if logFile != nil {
				_ = logFile.Close()
			}
		}()

		xcelerateParams.OrigArgs = os.Args[1:]

		silentLogging := config.Silent
		if slices.Contains(xcelerateParams.OrigArgs, "-json") {
			silentLogging = true
		}
		logOutput := wrapperLogWriter(logFile, logPath, silentLogging)
		logger := log.NewLogger(log.WithPrefix("[Bitrise Analytics] "), log.WithOutput(logOutput))
		cacheLogger := log.NewLogger(log.WithPrefix("[Bitrise Build Cache] "), log.WithOutput(logOutput))

		// Check for --no-bitrise-build-cache flag: override config and filter it out
		if slices.Contains(xcelerateParams.OrigArgs, NoBitriseBuildCacheFlag) {
			xcelerateParams.OrigArgs = slices.DeleteFunc(xcelerateParams.OrigArgs, func(s string) bool {
				return s == NoBitriseBuildCacheFlag
			})
			config.BuildCacheEnabled = false
			if !silentLogging {
				logger.TInfof(MsgBuildCacheDisabledByFlag, NoBitriseBuildCacheFlag)
			}
		}

		// Automatically disable cache for -create-xcframework as it's incompatible
		if slices.Contains(xcelerateParams.OrigArgs, CreateXCFrameworkFlag) {
			config.BuildCacheEnabled = false
			if !silentLogging {
				logger.TInfof(MsgBuildCacheDisabledByFlag, CreateXCFrameworkFlag)
			}
		}

		xcodeArgs := xcodeargs.NewDefault(
			cobraCmd,
			xcelerateParams.OrigArgs,
			logger,
		)

		logger.EnableDebugLog(config.DebugLogging)

		var proxySessionClient session.SessionClient
		if config.BuildCacheEnabled {
			logger.TInfof("Cache enabled, starting xcelerate proxy connecting to: %s", config.BuildCacheEndpoint)

			err := startProxy(
				logger,
				osProxy,
				utils.DefaultCommandFunc(),
				func(pid int, signum syscall.Signal) {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				},
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

		xcodeRunner := xcodeargs.NewRunner(logger, config, logFile)

		runner := &XcodebuildRunner{
			Config:             config,
			Metadata:           metadata,
			InvocationID:       invocationID,
			Logger:             logger,
			CacheLogger:        cacheLogger,
			XcodeRunner:        xcodeRunner,
			ProxySessionClient: proxySessionClient,
			XcodeArgs:          xcodeArgs,
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

type invocationSaver interface {
	PutInvocation(inv analytics.Invocation) error
}

type relationSender interface {
	PutInvocationRelation(rel multiplatform.InvocationRelation) error
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

	// invocationAPI saves invocations. If nil, a production analytics client is created.
	invocationAPI invocationSaver
	// relationAPI sends invocation relations. If nil, a production multiplatform client is created.
	relationAPI relationSender
}

// Run executes the xcodebuild wrapper: runs xcodebuild, collects stats,
// sends analytics, and registers invocation relations.
//
//nolint:nestif
func (c *XcodebuildRunner) Run(ctx context.Context) xcodeargs.RunStats {
	toPass := getArgsToPass(c.Config, c.XcodeArgs)
	c.Logger.TDebugf(MsgArgsPassedToXcodebuild, toPass)

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

	c.saveInvocationAndRelation(*inv)

	return runStats
}

func (c *XcodebuildRunner) saveInvocationAndRelation(inv analytics.Invocation) {
	saver, err := c.resolveInvocationAPI()
	if err != nil {
		c.Logger.Errorf("Failed to create analytics client: %v", err)

		return
	}

	if err := saver.PutInvocation(inv); err != nil {
		c.Logger.Errorf("Failed to send invocation analytics: %v", err)

		return
	}

	c.Logger.TInfof(MsgInvocationSaved, c.InvocationID)

	if parentID := os.Getenv("BITRISE_INVOCATION_ID"); parentID != "" {
		c.sendRelation(parentID)
	}
}

func (c *XcodebuildRunner) resolveInvocationAPI() (invocationSaver, error) {
	if c.invocationAPI != nil {
		return c.invocationAPI, nil
	}

	client, err := analytics.NewClient(consts.AnalyticsServiceEndpoint, c.Config.AuthConfig.TokenInGradleFormat(), c.Logger)
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
// 1. BITRISE_BUILD_CACHE_BENCHMARK_PHASE env var (set during activation)
// 2. ~/.local/state/xcelerate/benchmark/benchmark-phase.json (file fallback)
func resolveBenchmarkPhase(logger log.Logger) string {
	if phase := os.Getenv("BITRISE_BUILD_CACHE_BENCHMARK_PHASE"); phase != "" {
		logger.Debugf("Benchmark phase from env: %s", phase)

		return phase
	}

	if phase := common.ReadBenchmarkPhaseFile(logger); phase != "" {
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

func getArgsToPass(config xcelerate.Config, xcodeArgs xcodeargs.XcodeArgs) []string {
	additional := map[string]string{}

	if config.BuildCacheEnabled {
		additional["COMPILATION_CACHE_REMOTE_SERVICE_PATH"] = config.ProxySocketPath

		if !config.BuildCacheSkipFlags {
			maps.Copy(additional, xcodeargs.CacheArgs)
			diagnosticRemarks := "NO"
			if config.DebugLogging {
				diagnosticRemarks = "YES"
			}
			maps.Copy(additional, map[string]string{
				"COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS": diagnosticRemarks,
			})
		}
	}

	return xcodeArgs.Args(additional)
}

func startProxy(
	logger log.Logger,
	osProxy utils.OsProxy,
	commandFunc utils.CommandFunc,
	killFunc func(pid int, signum syscall.Signal),
) error {
	pidFilePth := xcelerate.PathFor(osProxy, pidFile)

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
