package cmd

import (
	"context"
	"os"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/session"
	"github.com/bitrise-io/bitrise-build-cache-cli/xcelerate/xcodeargs"
)

const (
	MsgArgsPassedToXcodebuild = "Arguments passed to xcodebuild: %v"

	ErrExecutingXcode = "Error executing xcodebuild: %v"
	ErrReadConfig     = "Error reading config: %v"
)

//go:generate moq -out mocks/runner_mock.go -pkg mocks . XcodeRunner
type XcodeRunner interface {
	Run(ctx context.Context, args []string) error
}

// rootCmd represents the base command when called without any subcommands
var xcodebuildCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "xcodebuild",
	Short: "TBD",
	Long: `xcodebuild -  Wrapper around xcodebuild to enable Bitrise Build Cache.
TBD`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(xcelerateParams.Debug)

		xcodeArgs := xcodeargs.NewDefault(
			cmd,
			xcelerateParams.OrigArgs,
			logger,
		)

		decoder := utils.DefaultDecoderFactory{}

		config, err := xcelerate.ReadConfig(utils.DefaultOsProxy{}, decoder)
		if err != nil {
			logger.Errorf(ErrReadConfig, err)
			config = xcelerate.DefaultConfig()
		}

		callProxySetSession(cmd.Context(), config, os.Getenv, logger)

		xcodeRunner := xcodeargs.NewRunner(logger, config)

		if err := XcodebuildCmdFn(cmd.Context(), logger, xcodeRunner, config, xcodeArgs); err != nil {
			logger.Errorf(ErrExecutingXcode, err)
		}

		return nil
	},
}

func init() {
	// IMPORTANT: silently skip flags not matching defined ones so we can pass them to xcodebuild
	xcodebuildCmd.FParseErrWhitelist = cobra.FParseErrWhitelist{UnknownFlags: true}
	rootCmd.AddCommand(xcodebuildCmd)
}

func XcodebuildCmdFn(
	ctx context.Context,
	logger log.Logger,
	xcodeRunner XcodeRunner,
	config xcelerate.Config,
	xcodeArgs xcodeargs.XcodeArgs,
) error {
	toPass := xcodeArgs.Args(map[string]string{
		"COMPILATION_CACHE_REMOTE_SERVICE_PATH": config.ProxySocketPath,
	})
	logger.TDebugf(MsgArgsPassedToXcodebuild, toPass)

	// Intentionally returning xcode error unwrapped

	//nolint:wrapcheck
	return xcodeRunner.Run(ctx, toPass)
}

func callProxySetSession(ctx context.Context, config xcelerate.Config, envProvider common.EnvProviderFunc, logger log.Logger) {
	proxySocket := "unix://" + strings.TrimPrefix(config.ProxySocketPath, "unix://")

	logger.TInfof("Connecting to proxy socket: %s", proxySocket)

	clientConn, err := grpc.NewClient(proxySocket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.TErrorf("Failed to create gRPC client: %v", err)

		return
	}
	defer clientConn.Close()

	_, err = session.NewSessionClient(clientConn).SetSession(ctx, &session.SetSessionRequest{
		InvocationId: uuid.New().String(),
		AppSlug:      envProvider("BITRISE_APP_SLUG"),
		BuildSlug:    envProvider("BITRISE_BUILD_SLUG"),
		StepSlug:     envProvider("BITRISE_STEP_EXECUTION_ID"),
	})
	if err != nil {
		logger.TErrorf("Failed to set session: %v", err)

		return
	}
}
