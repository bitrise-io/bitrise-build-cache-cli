package reactnative

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics/multiplatform"
	ccacheipc "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/analytics"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

// knownPackageManagers lists runners whose subcommand is meaningful enough to include
// in the Command field. For these, Command captures "runner subcommand" (two tokens)
// rather than just "runner".
//
//nolint:gochecknoglobals
var knownPackageManagers = map[string]bool{
	"yarn":     true,
	"npm":      true,
	"npx":      true,
	"expo":     true,
	"pnpm":     true,
	"fastlane": true,
}

// knownThreeTokenPrefixes lists (runner, subcommand) pairs where the third argument
// is also meaningful (e.g. "npm run <script>", "npx react-native <command>").
//
//nolint:gochecknoglobals
var knownThreeTokenPrefixes = map[[2]string]bool{
	{"npm", "run"}:          true,
	{"npx", "react-native"}: true,
}

// parseCommand derives the Command value from the raw argument list.
// For "npm run" and "npx react-native" it returns three tokens (e.g. "npm run build:ios").
// For other known package managers it returns two tokens (e.g. "yarn build:ios").
// For everything else it returns just the first argument.
func parseCommand(args []string) string {
	if len(args) == 0 {
		return ""
	}

	if len(args) > 2 && knownThreeTokenPrefixes[[2]string{args[0], args[1]}] {
		return args[0] + " " + args[1] + " " + args[2]
	}

	if len(args) > 1 && knownPackageManagers[args[0]] {
		return args[0] + " " + args[1]
	}

	return args[0]
}

// PostRunFn is called after the wrapped command completes with the invocation ID,
// original args, elapsed duration, and any execution error.
type PostRunFn func(invocationID string, args []string, duration time.Duration, execErr error)

// BuildPostRunFn constructs a PostRunFn with injectable dependencies.
// getMetadataFn returns system/CI metadata for the analytics payload.
// getAuthConfigFn returns the auth config used to identify the workspace.
// sendFn delivers the run-level (react-native) Invocation to the analytics backend.
// collectStatsFn, if non-nil, collects and zeros ccache stats after the run; it receives
// the run's invocationID as the parent ID for the ccache invocation.
func BuildPostRunFn(
	getMetadataFn func() common.CacheConfigMetadata,
	getAuthConfigFn func() (common.CacheAuthConfig, error),
	sendFn func(inv multiplatform.Invocation) error,
	collectStatsFn func(ctx context.Context, parentID string),
) PostRunFn {
	return func(invocationID string, args []string, duration time.Duration, execErr error) {
		authConfig, err := getAuthConfigFn()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get auth config for ccache analytics: %v\n", err)

			return
		}

		metadata := getMetadataFn()

		command := parseCommand(args)
		fullCommand := ""
		if len(args) > 0 {
			fullCommand = strings.Join(args, " ")
		}

		inv := multiplatform.NewInvocation(multiplatform.InvocationRunStats{
			InvocationDate: time.Now().Add(-duration),
			InvocationID:   invocationID,
			Duration:       duration,
			Command:        command,
			FullCommand:    fullCommand,
			Success:        execErr == nil,
			Error:          execErr,
			Wrapper:        "react-native",
		}, authConfig, metadata)

		if err := sendFn(*inv); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to send run invocation analytics: %v\n", err)
		}

		if collectStatsFn != nil {
			collectStatsFn(context.Background(), invocationID)
		}
	}
}

//nolint:gochecknoglobals
var defaultPostRunFn = BuildPostRunFn(
	func() common.CacheConfigMetadata {
		envs := utils.AllEnvs()
		logger := log.NewLogger()

		return common.NewMetadata(envs, func(name string, args ...string) (string, error) {
			out, err := exec.CommandContext(context.Background(), name, args...).Output() //nolint:gosec

			return string(out), err
		}, logger)
	},
	func() (common.CacheAuthConfig, error) {
		config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return common.CacheAuthConfig{}, fmt.Errorf("read ccache config: %w", err)
		}

		return config.AuthConfig, nil
	},
	func(inv multiplatform.Invocation) error {
		config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return fmt.Errorf("read ccache config: %w", err)
		}

		logger := log.NewLogger()

		client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
		if err != nil {
			return fmt.Errorf("create analytics client: %w", err)
		}

		// run-level (react-native) invocation data
		return client.PutInvocation(inv)
	},
	func(ctx context.Context, parentID string) {
		config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read ccache config for stats collection: %v\n", err)

			return
		}

		logger := log.NewLogger()

		var dl, ul int64
		if ccacheipc.IsListening(config.IPCEndpoint) {
			dl, ul, err = ccacheipc.SendGetSessionStats(ctx, config.IPCEndpoint)
			if err != nil {
				logger.TWarnf("Failed to get session stats from storage helper: %v", err)
			}
		}

		client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
		if err != nil {
			logger.TErrorf("Failed to create analytics client for ccache stats: %v", err)

			return
		}

		ccacheanalytics.CollectAndZero(ctx, client, uuid.New().String(), parentID, dl, ul, logger)
	},
)
