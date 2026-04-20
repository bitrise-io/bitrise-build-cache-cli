package reactnative

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/analytics/multiplatform"
	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/ccache/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/ccache"
)

// ---------------------------------------------------------------------------
// Private — post-run analytics
// ---------------------------------------------------------------------------

// postRunDeps handles post-run analytics: invocation reporting, ccache stats
// collection, and invocation relation registration.
type postRunDeps struct {
	logger     log.Logger
	authConfig common.CacheAuthConfig
	client     *ccacheanalytics.Client
}

func newPostRunDeps(logger log.Logger, osProxy utils.OsProxy, decoderFactory utils.DecoderFactory) *postRunDeps {
	config, err := multiplatformconfig.ReadConfig(osProxy, decoderFactory)
	if err != nil {
		logger.TWarnf("Failed to read multiplatform analytics config for post-run hook: %v", err)

		return nil
	}

	// Use a debug-enabled logger for the analytics client if activation was done
	// with --debug. The runner's logger (passed in) is NOT modified — we create
	// a separate client logger so HTTP PUT lines appear in the output when debug
	// mode was requested at activation time.
	clientLogger := log.NewLogger(log.WithDebugLog(config.DebugLogging))

	client, err := ccacheanalytics.NewClient(consts.MultiplatformAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), clientLogger)
	if err != nil {
		logger.TWarnf("Failed to create analytics client for post-run hook: %v", err)

		return nil
	}

	return &postRunDeps{
		logger:     logger,
		authConfig: config.AuthConfig,
		client:     client,
	}
}

// run sends invocation analytics, and if needed, collects ccache
// stats, and registers the invocation relation.
func (d *postRunDeps) run(ctx context.Context, wrapperInvocationID string, args []string, duration time.Duration, execErr error) {
	metadata := d.getMetadata()

	command := parseCommand(args)
	fullCommand := ""
	if len(args) > 0 {
		fullCommand = strings.Join(args, " ")
	}

	// Send wrapper invocation analytics
	inv := multiplatform.NewInvocation(multiplatform.InvocationRunStats{
		InvocationDate: time.Now().Add(-duration),
		InvocationID:   wrapperInvocationID,
		Duration:       duration,
		Command:        command,
		FullCommand:    fullCommand,
		Success:        execErr == nil,
		Error:          execErr,
		BuildTool:      "react-native",
		Wrapper:        "bitrise-build-cache-cli react-native",
	}, d.authConfig, metadata)

	rnSendErr := d.sendInvocation(*inv)
	if rnSendErr != nil {
		d.logger.TWarnf("Failed to send run invocation analytics: %v", rnSendErr)

		return
	}

	helper, err := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
		ParentInvocationID: wrapperInvocationID,
	})
	if err != nil {
		d.logger.TWarnf("Failed to create storage helper for ccache stats collection: %v", err)

		return
	}

	if err := helper.CollectAndSendStats(ctx, "", ""); err != nil {
		d.logger.TWarnf("Failed to collect and send ccache stats: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Private — postRunDeps methods
// ---------------------------------------------------------------------------

func (d *postRunDeps) getMetadata() common.CacheConfigMetadata {
	envs := utils.AllEnvs()

	return common.NewMetadata(envs, func(name string, args ...string) (string, error) {
		out, err := exec.CommandContext(context.Background(), name, args...).Output() //nolint:gosec

		return string(out), err
	}, d.logger)
}

func (d *postRunDeps) sendInvocation(inv multiplatform.Invocation) error {
	if err := d.client.PutInvocation(inv); err != nil {
		return fmt.Errorf("send invocation: %w", err)
	}

	return nil
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
