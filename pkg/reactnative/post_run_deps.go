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

// ---------------------------------------------------------------------------
// Private — post-run analytics hook
// ---------------------------------------------------------------------------

//go:generate moq -stub -out post_run_hook_mock_test.go -pkg reactnative . postRunHook

type postRunHook interface {
	getMetadata() common.CacheConfigMetadata
	getAuthConfig() (common.CacheAuthConfig, error)
	sendInvocation(inv multiplatform.Invocation) error
	collectStats(ctx context.Context, ccacheInvocationID, parentID string)
	sendRelation(ctx context.Context, parentID, childID string)
}

// postRunDeps implements postRunHook with production analytics backends.
type postRunDeps struct{}

func (d *postRunDeps) getMetadata() common.CacheConfigMetadata {
	envs := utils.AllEnvs()
	logger := log.NewLogger()

	return common.NewMetadata(envs, func(name string, args ...string) (string, error) {
		out, err := exec.CommandContext(context.Background(), name, args...).Output() //nolint:gosec

		return string(out), err
	}, logger)
}

func (d *postRunDeps) getAuthConfig() (common.CacheAuthConfig, error) {
	config, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	if err != nil {
		return common.CacheAuthConfig{}, fmt.Errorf("read multiplatform analytics config: %w", err)
	}

	return config.AuthConfig, nil
}

func (d *postRunDeps) sendInvocation(inv multiplatform.Invocation) error {
	config, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	if err != nil {
		return fmt.Errorf("read multiplatform analytics config: %w", err)
	}

	logger := log.NewLogger(log.WithDebugLog(config.DebugLogging))

	client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
	if err != nil {
		return fmt.Errorf("create analytics client: %w", err)
	}

	if err := client.PutInvocation(inv); err != nil {
		return fmt.Errorf("send invocation: %w", err)
	}

	return nil
}

func (d *postRunDeps) collectStats(ctx context.Context, ccacheInvocationID, parentID string) {
	osProxy := utils.DefaultOsProxy{}
	configPath := ccacheconfig.PathFor(osProxy, "config.json")
	if _, err := osProxy.Stat(configPath); err != nil {
		return
	}

	config, err := ccacheconfig.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to read ccache config for stats collection: %v\n", err)

		return
	}

	logger := log.NewLogger(log.WithDebugLog(config.DebugLogging))

	var dl, ul int64
	if ccacheipc.IsListening(config.IPCEndpoint) { //nolint:contextcheck // IsListening uses its own short-lived context
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
}

func (d *postRunDeps) sendRelation(ctx context.Context, parentID, childID string) {
	config, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to read config for invocation relation: %v\n", err)

		return
	}

	logger := log.NewLogger(log.WithDebugLog(config.DebugLogging))

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
}

func runPostHook(hook postRunHook, invocationID string, args []string, duration time.Duration, execErr error, ccacheInvocationID string) {
	authConfig, err := hook.getAuthConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get auth config for ccache analytics: %v\n", err)

		return
	}

	metadata := hook.getMetadata()

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

	if ccacheInvocationID == "" {
		ccacheInvocationID = uuid.New().String()
	}

	rnSendErr := hook.sendInvocation(*inv)
	if rnSendErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to send run invocation analytics: %v\n", rnSendErr)
	}

	osProxy := utils.DefaultOsProxy{}
	ccacheConfigPath := ccacheconfig.PathFor(osProxy, "config.json")
	if _, statErr := osProxy.Stat(ccacheConfigPath); statErr != nil {
		return
	}

	fmt.Fprintf(os.Stderr, "Ccache invocation ID: %s\n", ccacheInvocationID)
	hook.collectStats(context.Background(), ccacheInvocationID, invocationID)

	if rnSendErr == nil {
		relParentID := os.Getenv("BITRISE_INVOCATION_ID")
		if relParentID == "" {
			relParentID = invocationID
		}

		fmt.Fprintf(os.Stderr, "Parent invocation ID: %s\n", relParentID)
		hook.sendRelation(context.Background(), relParentID, ccacheInvocationID)
	}
}

// ---------------------------------------------------------------------------
// Private — package-level helpers
// ---------------------------------------------------------------------------

//nolint:gochecknoglobals
var knownPackageManagers = map[string]bool{
	"yarn":     true,
	"npm":      true,
	"npx":      true,
	"expo":     true,
	"pnpm":     true,
	"fastlane": true,
}

//nolint:gochecknoglobals
var knownThreeTokenPrefixes = map[[2]string]bool{
	{"npm", "run"}:          true,
	{"npx", "react-native"}: true,
}

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
