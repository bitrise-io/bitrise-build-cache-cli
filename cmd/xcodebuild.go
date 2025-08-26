package cmd

import (
	"context"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/xcodeargs"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/session"
	"github.com/golang/protobuf/ptypes/empty"
)

const (
	MsgArgsPassedToXcodebuild = "Arguments passed to xcodebuild: %v"
	MsgInvocationSuccess      = "Invocation succeeded ✅ after %.2f seconds"
	MsgInvocationFailed       = "Invocation failed ❌ after %.2f seconds: %s"
	MsgInvocationSaved        = "Invocation data saved"

	ErrExecutingXcode = "Error executing xcodebuild: %v"
	ErrReadConfig     = "Error reading config: %v"
)

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
	DisableFlagParsing: true,
	RunE: func(cobraCmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()

		xcelerateParams.OrigArgs = os.Args[1:]

		xcodeArgs := xcodeargs.NewDefault(
			cobraCmd,
			xcelerateParams.OrigArgs,
			logger,
		)

		decoder := utils.DefaultDecoderFactory{}

		config, err := xcelerate.ReadConfig(utils.DefaultOsProxy{}, decoder)
		if err != nil {
			logger.Errorf(ErrReadConfig, err)
			config = xcelerate.DefaultConfig()
		}
		logger.EnableDebugLog(config.DebugLogging)

		var proxySessionClient session.SessionClient
		if config.BuildCacheEnabled {
			var cleanup func()

			proxySessionClient, cleanup = createProxySessionClient(config, logger)
			defer cleanup()
		}

		metadata := common.NewMetadata(os.Getenv, func(cmd string, args ...string) (string, error) {
			o, err := utils.DefaultCommandFunc()(cobraCmd.Context(), cmd, args...).CombinedOutput()

			return string(o), err
		}, logger)

		xcodeRunner := xcodeargs.NewRunner(logger, config)

		if err := XcodebuildCmdFn(cobraCmd.Context(), logger, xcodeRunner, proxySessionClient, config, metadata, xcodeArgs); err != nil {
			logger.Errorf(ErrExecutingXcode, err)
		}

		return nil
	},
}

func init() {
	// IMPORTANT: silently skip flags not matching defined ones so we can pass them to xcodebuild
	xcelerateCommand.AddCommand(xcodebuildCmd)
}

func XcodebuildCmdFn(
	ctx context.Context,
	logger log.Logger,
	xcodeRunner XcodeRunner,
	proxySessionClient session.SessionClient,
	config xcelerate.Config,
	metadata common.CacheConfigMetadata,
	xcodeArgs xcodeargs.XcodeArgs,
) error {
	toPass := getArgsToPass(config, xcodeArgs)
	logger.TDebugf(MsgArgsPassedToXcodebuild, toPass)

	invocationID := uuid.New().String()

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
	}

	runStats := xcodeRunner.Run(ctx, toPass)
	if runStats.Error != nil {
		logger.TErrorf(MsgInvocationFailed, (time.Duration(runStats.DurationMS) * time.Millisecond).Seconds(), runStats.Error)
	} else {
		logger.TDonef(MsgInvocationSuccess, (time.Duration(runStats.DurationMS) * time.Millisecond).Seconds())
	}
	logger.Debugf("Run stats: %+v", runStats)

	var hitRate float32
	if proxySessionClient != nil {
		proxyStats, err := proxySessionClient.GetSessionStats(ctx, &empty.Empty{})
		if err != nil {
			logger.TErrorf("Failed to get session stats: %v", err)
		}
		logger.Debugf("Proxy stats: %+v", proxyStats)

		hitRate = float32(proxyStats.GetHits()) / float32(proxyStats.GetHits()+proxyStats.GetMisses())
	}

	inv := analytics.NewInvocation(analytics.InvocationRunStats{
		InvocationDate: runStats.StartTime,
		InvocationID:   invocationID,
		Duration:       runStats.DurationMS,
		HitRate:        hitRate,
		Command:        xcodeArgs.ShortCommand(),
		FullCommand:    xcodeArgs.Command(),
		Success:        runStats.Success,
		Error:          runStats.Error,
		XcodeVersion:   "", // TODO from logs
	}, config.AuthConfig, metadata)

	client, err := analytics.NewClient(consts.AnalyticsServiceEndpoint, config.AuthConfig.AuthToken, logger)
	if err != nil {
		logger.Errorf("Failed to create analytics client: %v", err)

		return runStats.Error
	}

	if err = client.PutInvocation(*inv); err != nil {
		logger.Errorf("Failed to send invocation analytics: %v", err)
	}
	logger.TInfof(MsgInvocationSaved)

	return runStats.Error
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
