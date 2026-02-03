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

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/xcodeargs"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/session"
)

const (
	pidFile      = "proxy.pid"
	startedProxy = "Started xcelerate_proxy pid = %d"

	MsgArgsPassedToXcodebuild = "Arguments passed to xcodebuild: %v"
	MsgInvocationSuccess      = "Invocation succeeded ✅ after %s"
	MsgInvocationFailed       = "Invocation failed ❌ after %s: %s"
	MsgInvocationSaved        = "Invocation saved. Visit 👉 https://app.bitrise.io/build-cache/invocations/xcode/%s"

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

		if runStats := XcodebuildCmdFn(
			cobraCmd.Context(),
			invocationID,
			logger,
			cacheLogger,
			xcodeRunner,
			proxySessionClient,
			config,
			metadata,
			xcodeArgs); runStats.Error != nil {
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

// nolint:nestif
func XcodebuildCmdFn(
	ctx context.Context,
	invocationID string,
	logger log.Logger,
	cacheLogger log.Logger,
	xcodeRunner XcodeRunner,
	proxySessionClient session.SessionClient,
	config xcelerate.Config,
	metadata common.CacheConfigMetadata,
	xcodeArgs xcodeargs.XcodeArgs,
) xcodeargs.RunStats {
	toPass := getArgsToPass(config, xcodeArgs)
	logger.TDebugf(MsgArgsPassedToXcodebuild, toPass)

	if proxySessionClient != nil {
		_, err := proxySessionClient.SetSession(ctx, &session.SetSessionRequest{
			InvocationId: invocationID,
			AppSlug:      metadata.BitriseAppID,
			BuildSlug:    metadata.BitriseBuildID,
			StepSlug:     metadata.BitriseStepExecutionID,
		})
		if err != nil {
			logger.TErrorf("Failed to set session: %v", err)
		}

		if !config.Silent {
			logger.Debugf("Streaming proxy logs...")
			proxyCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			go func() {
				if err := streamProxyLogs(proxyCtx, invocationID, cacheLogger, utils.DefaultOsProxy{}); err != nil {
					logger.Errorf("Failed to stream proxy logs: %v", err)
				} else {
					logger.Debugf("Proxy logs stream closed")
				}
			}()
		}
	}

	runStats := xcodeRunner.Run(ctx, toPass)
	if runStats.Error != nil {
		logger.TErrorf(MsgInvocationFailed, time.Duration(runStats.DurationMS)*time.Millisecond, runStats.Error)
	} else {
		logger.TDonef(MsgInvocationSuccess, time.Duration(runStats.DurationMS)*time.Millisecond)
	}
	logger.Debugf("Run stats: %+v", runStats)

	hitRate := getHitRateFromSessionAndRunStats(ctx, proxySessionClient, runStats, logger)

	if !shouldSaveInvocation(xcodeArgs) {
		logger.TDebugf("Save invocation skipped")

		return runStats
	}

	inv := analytics.NewInvocation(analytics.InvocationRunStats{
		InvocationDate:   runStats.StartTime,
		InvocationID:     invocationID,
		Duration:         runStats.DurationMS,
		HitRate:          hitRate,
		Command:          xcodeArgs.ShortCommand(),
		FullCommand:      xcodeArgs.Command(),
		Success:          runStats.Success,
		Error:            runStats.Error,
		XcodeVersion:     runStats.XcodeVersion,
		XcodeBuildNumber: runStats.XcodeBuildNumber,
	}, config.AuthConfig, metadata)

	client, err := analytics.NewClient(consts.AnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
	if err != nil {
		logger.Errorf("Failed to create analytics client: %v", err)

		return runStats
	}

	if err = client.PutInvocation(*inv); err != nil {
		logger.Errorf("Failed to send invocation analytics: %v", err)
	} else {
		logger.TInfof(MsgInvocationSaved, invocationID)
	}

	return runStats
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
