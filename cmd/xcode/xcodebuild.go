package xcode

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
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

		logOutput, cleanup := logFile(invocationID, utils.DefaultOsProxy{}, utils.AllEnvs(), config.Silent)
		defer cleanup()
		logger := log.NewLogger(log.WithPrefix("[Bitrise Analytics] "), log.WithOutput(logOutput))

		xcelerateParams.OrigArgs = os.Args[1:]

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

		xcodeRunner := xcodeargs.NewRunner(logger, config)

		if runStats := XcodebuildCmdFn(
			cobraCmd.Context(),
			invocationID,
			logger,
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

func logFile(invocationID string, osProxy utils.OsProxy, envs map[string]string, silent bool) (io.Writer, func()) {
	deployDir := envs["BITRISE_DEPLOY_DIR"]
	var logDir string
	if deployDir != "" {
		logDir = deployDir
	} else {
		var err error
		logDir, err = getLogDir(osProxy)
		if err != nil {
			return os.Stderr, func() {}
		}
	}

	logPath := fmt.Sprintf("%s/xcelerate-%s.log", logDir, invocationID)
	f, err := osProxy.Create(logPath)
	if err != nil {
		if silent {
			return io.Discard, func() {}
		}
		return os.Stderr, func() {}
	}

	// wrap stderr to also write to log file
	var w io.Writer
	if silent {
		w = f
	} else {
		//nolint:errcheck
		fmt.Fprintf(os.Stderr, "ℹ️ These logs are available at: %s\n", logPath)

		w = io.MultiWriter(os.Stderr, f)
	}

	return w, func() {
		if err := f.Close(); err != nil {
			//nolint:errcheck
			fmt.Fprintf(os.Stderr, "Failed to close log file: %v\n", err)
		}
	}
}

func XcodebuildCmdFn(
	ctx context.Context,
	invocationID string,
	logger log.Logger,
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
			proxyCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			go func() {
				if err := streamProxyLogs(proxyCtx, invocationID, logger, utils.DefaultOsProxy{}); err != nil {
					logger.Errorf("Failed to stream proxy logs: %v", err)
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

	var hitRate float32
	if proxySessionClient != nil {
		proxyStats, err := proxySessionClient.GetSessionStats(ctx, &empty.Empty{})
		if err != nil {
			logger.TErrorf("Failed to get session stats: %v", err)
		}
		logger.Infof(
			"Proxy stats: uploaded bytes: %s, downloaded bytes: %s, hits: %d, misses: %d",
			humanize.Bytes(uint64(proxyStats.GetUploadedBytes())),   // nolint: gosec
			humanize.Bytes(uint64(proxyStats.GetDownloadedBytes())), // nolint: gosec
			proxyStats.GetHits(),
			proxyStats.GetMisses(),
		)

		// hit rate is hits / (hits + misses)
		if proxyStats.GetHits()+proxyStats.GetMisses() > 0 {
			hitRate = float32(proxyStats.GetHits()) / float32(proxyStats.GetHits()+proxyStats.GetMisses())
		}
	}

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
		additional = maps.Clone(xcodeargs.CacheArgs)
		maps.Copy(additional, map[string]string{
			"COMPILATION_CACHE_REMOTE_SERVICE_PATH": config.ProxySocketPath,
		})
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

		return nil, true
	})
	if err != nil {
		return fmt.Errorf("failed to open proxy log file after retries: %w", err)
	}

	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			logger.Errorf("failed to close log file: %v", err)
		}
	}(f)

	r := bufio.NewReader(f)
	for {
		select {
		case <-ctx.Done():
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
		// To have a different prefix than the wrapper
		// nolint: forbidigo
		fmt.Fprintln(os.Stderr, "[Bitrise Build Cache] "+strings.TrimSpace(line))
	}
}
