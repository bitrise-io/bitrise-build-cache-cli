package xcode

import (
	"context"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/deriveddata"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/collect"
	xcprobepayload "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

// enrichmentRunFn is a swap seam for hook-site tests that need to bypass
// xcprobe.Run (shelling out, network).
//
//nolint:gochecknoglobals
var enrichmentRunFn = func(
	ctx context.Context,
	opts xceleratconfig.EnrichmentOptions,
	in collect.Input,
	logger log.Logger,
	inv *analytics.Invocation,
) {
	out := xcprobe.Run(ctx, opts, in, logger)
	if err := xcprobepayload.AttachToInvocation(inv, out); err != nil {
		logger.Warnf("Failed to attach xcprobe enrichment to invocation %s: %v", inv.InvocationID, err)
	}
}

// newEnrichmentHook builds the IDE-side EnrichmentHookFn. Function-shaped
// injection keeps enricher.go off internal/config/xcelerate (avoids a cycle
// with flush_session.go).
func newEnrichmentHook(logger log.Logger) enrichment.EnrichmentHookFn {
	return func(
		ctx context.Context,
		manifestPath string,
		group enrichment.ManifestEntryGroup,
		host configcommon.HostMetadata,
		benchmarkPhase string,
		inv *analytics.Invocation,
	) {
		opts := xceleratconfig.LoadEnrichmentOptions(os.Getenv, xceleratconfig.EnrichmentFlags{})
		if !opts.AnyEnabled() {
			return
		}

		in := collect.Input{
			DerivedDataPath: deriveddata.DDRootFromManifestPath(manifestPath),
			Scheme:          group.SchemeName(),
			WindowStart:     group.Start(),
			WindowEnd:       group.Stop(),
			HostBase:        host,
			BenchmarkPhase:  benchmarkPhase,
		}

		enrichmentRunFn(ctx, opts, in, logger, inv)
	}
}
