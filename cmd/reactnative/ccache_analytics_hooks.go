package reactnative

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/analytics"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

// PostRunFn is called after the wrapped command completes with the invocation ID,
// original args, elapsed duration, and any execution error.
type PostRunFn func(invocationID string, args []string, duration time.Duration, execErr error)

// BuildPostRunFn constructs a PostRunFn with injectable dependencies.
// getMetadataFn returns system/CI metadata for the analytics payload.
// getAuthConfigFn returns the auth config used to identify the workspace.
// sendFn delivers the run-level (react-native) Invocation to the analytics backend.
func BuildPostRunFn(
	getMetadataFn func() common.CacheConfigMetadata,
	getAuthConfigFn func() (common.CacheAuthConfig, error),
	sendFn func(inv ccacheanalytics.Invocation) error,
) PostRunFn {
	return func(invocationID string, args []string, duration time.Duration, execErr error) {
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

		// run-level (react-native) invocation data
		return client.PutInvocation(inv)
	},
)
