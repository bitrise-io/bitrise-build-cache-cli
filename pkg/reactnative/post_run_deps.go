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
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/common/childstats"
)

// ---------------------------------------------------------------------------
// Private — post-run analytics
// ---------------------------------------------------------------------------

// MsgRNInvocationSaved is the log line emitted after the BE confirms the
// React Native wrapper invocation was persisted. Format args: pre-formatted
// URL pointing at the per-invocation details page in the Bitrise app.
// Use rnInvocationDetailsURL to build the URL — the workspace slug is
// required for the page to render (without it the route 404s for
// react-native, see ACI-4923).
const MsgRNInvocationSaved = "React Native invocation saved. Visit 👉 %s"

// rnInvocationDetailsURL returns the public details URL for a React Native
// invocation. The workspace slug is required: the route 404s without it for
// the react-native build-tool (gradle / xcode happen to redirect through
// session, but RN does not). Callers must only invoke this once the runner
// has confirmed activation is complete and the workspace slug is set.
func rnInvocationDetailsURL(workspaceSlug, invocationID string) string {
	return fmt.Sprintf("https://app.bitrise.io/build-cache/%s/invocations/react-native/%s", workspaceSlug, invocationID)
}

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

	client, err := ccacheanalytics.NewClient(consts.MultiplatformAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
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

// run collects ccache stats, aggregates child invocation hit rates into
// the wrapper invocation, and sends the wrapper analytics. ccache collection
// runs first so its ledger entry can contribute to the aggregated hit rate.
func (d *postRunDeps) run(ctx context.Context, wrapperInvocationID string, args []string, duration time.Duration, execErr error) {
	metadata := d.getMetadata()

	command := parseCommand(args)
	fullCommand := ""
	if len(args) > 0 {
		fullCommand = strings.Join(args, " ")
	}

	// Mirror the activate-side skip: when Gradle is in benchmark baseline,
	// activate-react-native deliberately does not write ccache config (see
	// Activator.activateCppIfApplicable). Attempting to load the helper here
	// would fail with a scary ENOENT WARN even though the skip is intentional.
	// Stop-gap until ccache grows its own benchmark phase support (ACI-4926).
	if common.ReadBenchmarkPhaseFile(common.BuildToolGradle, d.logger) == common.BenchmarkPhaseBaseline {
		d.logger.Debugf("Skipping ccache stats collection: Gradle is in benchmark baseline mode.")
	} else {
		helper, helperErr := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
			ParentInvocationID: wrapperInvocationID,
		})
		if helperErr != nil {
			d.logger.TWarnf("Failed to create storage helper for ccache stats collection: %v", helperErr)
		} else {
			helper.CollectAndSendStats(ctx, "", "")
		}
	}

	agg := childstats.NewAggregator(wrapperInvocationID)
	agg.Logger = d.logger
	summary, aggErr := agg.Compute()
	switch {
	case aggErr != nil:
		d.logger.TWarnf("Failed to aggregate child invocation hit rates: %v", aggErr)
	case summary.ChildCount > 0:
		d.logger.TInfof(
			"Cache hit rate (avg of %d child invocations): %.1f%%",
			summary.ChildCount, summary.MeanHitRate*100,
		)

		if summary.TotalCount > 0 {
			d.logger.TInfof(
				"Cache hit rate (total ratio %d/%d across child invocations): %.1f%%",
				summary.TotalHits, summary.TotalCount, summary.WeightedHitRate*100,
			)
		}

		if summary.BaselineCount > 0 {
			d.logger.TWarnf(
				"%d of %d child invocations ran in baseline benchmark mode (cache disabled) — they pull the average down.",
				summary.BaselineCount, summary.ChildCount,
			)
		}

		if summary.NoActivityCount > 0 {
			d.logger.TInfof(
				"%d child invocation(s) excluded from the aggregate: no cache activity.",
				summary.NoActivityCount,
			)
		}

		if summary.FailedCount > 0 {
			d.logger.TInfof(
				"%d of %d child invocations reported a failure (informational; wrapper success is driven by the wrapped process exit code).",
				summary.FailedCount, summary.ChildCount,
			)
		}
	case summary.NoActivityCount > 0:
		// All children present but none had activity — log it so the user
		// understands why no aggregate hit rate is shown.
		d.logger.TInfof(
			"All %d child invocation(s) had no cache activity; skipping combined hit rate.",
			summary.NoActivityCount,
		)
	}

	// Wrapper Success follows the wrapped process's exit code only. We used to
	// also fail the wrapper when any child reported Failed=true, but child
	// failure flags are noisy (e.g. ccache marks a run failed on user-side
	// compile errors that don't affect ccache itself) and made successful
	// builds report as failed wrappers. FailedCount stays in the summary for
	// observability; the only signal that flips Success here is execErr.
	wrapperSuccess := execErr == nil

	// Report the child invocations (gradle/xcode/ccache) inline so the backend
	// gets the full parent→child lineage from the wrapper invocation itself,
	// without a separate relation call per child.
	children := make([]multiplatform.InvocationRef, 0, len(summary.Children))
	for _, c := range summary.Children {
		children = append(children, multiplatform.InvocationRef{
			InvocationID: c.InvocationID,
			BuildTool:    c.BuildTool,
		})
		d.logger.TInfof("Reporting child invocation %s (build tool: %s) on the react-native invocation", c.InvocationID, c.BuildTool)
	}

	inv := multiplatform.NewInvocation(multiplatform.InvocationRunStats{
		InvocationDate: time.Now().Add(-duration),
		InvocationID:   wrapperInvocationID,
		Duration:       duration,
		Command:        command,
		FullCommand:    fullCommand,
		Success:        wrapperSuccess,
		Error:          execErr,
		BuildTool:      "react-native",
		Wrapper:        "bitrise-build-cache-cli react-native",
		HitRate:        summary.MeanHitRate,
		Children:       children,
	}, d.authConfig, metadata)

	if err := d.sendInvocation(*inv); err != nil {
		d.logger.TWarnf("Failed to send run invocation analytics: %v", err)
	} else {
		// BE confirmed the invocation was stored — surface the details URL
		// so users can jump to it from the build log.
		d.logger.TInfof(MsgRNInvocationSaved, rnInvocationDetailsURL(d.authConfig.WorkspaceID, wrapperInvocationID))
	}

	if err := agg.Cleanup(); err != nil {
		d.logger.TWarnf("Failed to clean up child stats ledger: %v", err)
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
