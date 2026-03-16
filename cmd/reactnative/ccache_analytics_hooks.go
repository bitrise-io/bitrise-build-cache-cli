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

	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/analytics"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

// CcacheAnalyticsHooks contains hooks that run before and after the wrapped command
// to collect ccache statistics and send them to the analytics backend.
type CcacheAnalyticsHooks struct {
	PreRun  func()
	PostRun func(invocationID string, args []string, duration time.Duration, execErr error)
}

// BuildCcacheAnalyticsHooksFn constructs a CcacheAnalyticsHooks with injectable dependencies.
// findCcacheFn locates the ccache binary; if it returns false, ccache-specific hooks are skipped.
// resetStatsFn resets ccache counters before the build (ccache -z).
// collectStatsFn fetches JSON stats after the build (ccache --print-stats --format=json).
// getMetadataFn returns system/CI metadata for the analytics payload.
// getAuthConfigFn returns the auth config used to identify the workspace.
// sendFn delivers the run-level Invocation (always sent, even without ccache).
// sendCcacheFn delivers the CcacheInvocation (only sent when ccache is present).
func BuildCcacheAnalyticsHooksFn(
	findCcacheFn func() (string, bool),
	resetStatsFn func(ccachePath string) error,
	collectStatsFn func(ccachePath string) ([]byte, error),
	getMetadataFn func() common.CacheConfigMetadata,
	getAuthConfigFn func() (common.CacheAuthConfig, error),
	sendFn func(inv ccacheanalytics.Invocation) error,
	sendCcacheFn func(inv ccacheanalytics.CcacheInvocation) error,
) *CcacheAnalyticsHooks {
	var ccachePath string

	return &CcacheAnalyticsHooks{
		PreRun: func() {
			path, ok := findCcacheFn()
			if !ok {
				return
			}

			ccachePath = path

			if err := resetStatsFn(path); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to reset ccache stats: %v\n", err)
			}
		},

		PostRun: func(invocationID string, args []string, duration time.Duration, execErr error) {
			authConfig, err := getAuthConfigFn()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get auth config for ccache analytics: %v\n", err)

				return
			}

			metadata := getMetadataFn()

			command := ""
			fullCommand := ""
			if len(args) > 0 {
				command = args[0]
				fullCommand = strings.Join(args, " ")
			}

			inv := ccacheanalytics.NewInvocation(ccacheanalytics.InvocationRunStats{
				InvocationDate: time.Now().Add(-duration),
				InvocationID:   invocationID,
				Duration:       duration,
				Command:        command,
				FullCommand:    fullCommand,
				Success:        execErr == nil,
				Error:          execErr,
			}, authConfig, metadata)

			if err := sendFn(*inv); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to send run invocation analytics: %v\n", err)
			}

			if ccachePath == "" {
				return // ccache was not found during PreRun
			}

			statsData, err := collectStatsFn(ccachePath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to collect ccache stats: %v\n", err)

				return
			}

			ccacheStats, err := ccacheanalytics.ParseCcacheStats(statsData)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to parse ccache stats: %v\n", err)

				return
			}

			ccacheInv := ccacheanalytics.NewCcacheInvocation(
				uuid.New().String(),
				invocationID,
				time.Now().Add(-duration),
				ccacheStats,
			)

			if err := sendCcacheFn(*ccacheInv); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to send ccache analytics: %v\n", err)
			}
		},
	}
}

//nolint:gochecknoglobals
var defaultCcacheAnalyticsHooks = BuildCcacheAnalyticsHooksFn(
	func() (string, bool) {
		path, err := exec.LookPath("ccache")

		return path, err == nil
	},
	func(ccachePath string) error {
		return exec.CommandContext(context.Background(), ccachePath, "-z").Run() //nolint:gosec
	},
	func(ccachePath string) ([]byte, error) {
		return exec.CommandContext(context.Background(), ccachePath, "--print-stats", "--format=json").Output() //nolint:gosec
	},
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
	func(inv ccacheanalytics.Invocation) error {
		config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return fmt.Errorf("read ccache config: %w", err)
		}

		logger := log.NewLogger()

		client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
		if err != nil {
			return fmt.Errorf("create analytics client: %w", err)
		}

		return client.PutInvocation(inv)
	},
	func(inv ccacheanalytics.CcacheInvocation) error {
		config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return fmt.Errorf("read ccache config: %w", err)
		}

		logger := log.NewLogger()

		client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
		if err != nil {
			return fmt.Errorf("create analytics client: %w", err)
		}

		return client.PutCcacheInvocation(inv)
	},
)
