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
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/multiplatform"
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

// PostRunDeps holds the injectable dependencies for building a PostRunFn.
type PostRunDeps struct {
	// GetMetadata returns system/CI metadata for the analytics payload.
	GetMetadata func() common.CacheConfigMetadata
	// GetAuthConfig returns the auth config used to identify the workspace.
	GetAuthConfig func() (common.CacheAuthConfig, error)
	// Send delivers the run-level (react-native) Invocation to the analytics backend.
	Send func(inv multiplatform.Invocation) error
	// CollectStats, if non-nil, collects and zeros ccache stats after the run; it receives
	// the pre-generated ccache invocation ID and the run's invocation ID as the parent.
	CollectStats func(ctx context.Context, ccacheInvocationID, parentID string)
	// SendRelation, if non-nil, is called after Send succeeds to register the parent→child
	// relationship between the run invocation and the ccache invocation. The parent ID is taken
	// from the BITRISE_INVOCATION_ID environment variable if set (outer context), otherwise the
	// run's own invocation ID is used. The child ID is the pre-generated ccache invocation ID.
	SendRelation func(ctx context.Context, parentID, childID string)
	// CcacheInvocationID is the ccache invocation ID shared with the pre-run hook so the server's
	// session stats and the analytics payload reference the same ID. If empty, a new UUID is generated.
	CcacheInvocationID string
}

// Build constructs a PostRunFn from the deps.
func (d PostRunDeps) Build() PostRunFn {
	return func(invocationID string, args []string, duration time.Duration, execErr error) {
		var authConfig common.CacheAuthConfig
		if d.GetAuthConfig != nil {
			var err error
			authConfig, err = d.GetAuthConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get auth config for ccache analytics: %v\n", err)

				return
			}
		}

		var metadata common.CacheConfigMetadata
		if d.GetMetadata != nil {
			metadata = d.GetMetadata()
		}

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
			BuildTool:      "react-native",
			Wrapper:        "bitrise-build-cache-cli react-native",
		}, authConfig, metadata)

		ccacheInvocationID := d.CcacheInvocationID
		if ccacheInvocationID == "" {
			ccacheInvocationID = uuid.New().String()
		}

		var rnSendErr error
		if d.Send == nil {
			rnSendErr = fmt.Errorf("analytics sender is nil")
		} else {
			rnSendErr = d.Send(*inv)
		}
		if rnSendErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to send run invocation analytics: %v\n", rnSendErr)
		}

		// Ccache stats collection and relation registration only apply when C++ cache was activated.
		osProxy := utils.DefaultOsProxy{}
		ccacheConfigPath := ccacheconfig.PathFor(osProxy, "config.json")
		if _, statErr := osProxy.Stat(ccacheConfigPath); statErr != nil {
			return
		}

		fmt.Fprintf(os.Stderr, "Ccache invocation ID: %s\n", ccacheInvocationID)

		if d.CollectStats != nil {
			d.CollectStats(context.Background(), ccacheInvocationID, invocationID)
		}

		if rnSendErr == nil && d.SendRelation != nil {
			relParentID := os.Getenv("BITRISE_INVOCATION_ID")
			if relParentID == "" {
				relParentID = invocationID
			}
			fmt.Fprintf(os.Stderr, "Parent invocation ID: %s\n", relParentID)

			d.SendRelation(context.Background(), relParentID, ccacheInvocationID)
		}
	}
}

//nolint:gochecknoglobals
var defaultPostRunDeps = PostRunDeps{
	GetMetadata: func() common.CacheConfigMetadata {
		envs := utils.AllEnvs()
		logger := log.NewLogger()

		return common.NewMetadata(envs, func(name string, args ...string) (string, error) {
			out, err := exec.CommandContext(context.Background(), name, args...).Output() //nolint:gosec

			return string(out), err
		}, logger)
	},
	GetAuthConfig: func() (common.CacheAuthConfig, error) {
		config, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return common.CacheAuthConfig{}, fmt.Errorf("read multiplatform analytics config: %w", err)
		}

		return config.AuthConfig, nil
	},
	Send: func(inv multiplatform.Invocation) error {
		config, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return fmt.Errorf("read multiplatform analytics config: %w", err)
		}

		logger := log.NewLogger()

		client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
		if err != nil {
			return fmt.Errorf("create analytics client: %w", err)
		}

		// run-level (react-native) invocation data
		return client.PutInvocation(inv)
	},
	CollectStats: func(ctx context.Context, ccacheInvocationID, parentID string) {
		osProxy := utils.DefaultOsProxy{}
		configPath := ccacheconfig.PathFor(osProxy, "config.json")
		if _, err := osProxy.Stat(configPath); err != nil {
			// No ccache config — C++ cache was not activated (e.g. Xcode-only build).
			return
		}

		config, err := ccacheconfig.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
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

		ccacheanalytics.CollectAndZero(ctx, client, ccacheInvocationID, parentID, dl, ul, logger)
	},
	SendRelation: func(ctx context.Context, parentID, childID string) {
		config, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read config for invocation relation: %v\n", err)

			return
		}

		logger := log.NewLogger()

		client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create analytics client for invocation relation: %v\n", err)

			return
		}

		rel := multiplatform.InvocationRelation{
			ParentInvocationID: parentID,
			ChildInvocationID:  childID,
			InvocationDate:     time.Now(),
			BuildTool:          "ccache",
		}

		if err := client.PutInvocationRelation(rel); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to register invocation relation: %v\n", err)
		}
	},
}
