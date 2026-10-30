package xcode

import (
	"context"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/collect"
	xcprobepayload "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

// newEnrichmentHook returns an EnrichmentHookFn that reads EnrichmentOptions
// at hook time, short-circuits when nothing is enabled, and otherwise runs
// the xcprobe aggregator and attaches the payload. Injected into
// enrichment.Enricher at construction so enricher.go stays off the
// internal/config/xcelerate import graph (would create a cycle with
// flush_session.go).
func newEnrichmentHook(logger log.Logger) enrichment.EnrichmentHookFn {
	return func(
		ctx context.Context,
		manifestPath string,
		group enrichment.ManifestEntryGroup,
		host configcommon.HostMetadata,
		benchmarkPhase string,
		inv *analytics.Invocation,
	) {
		opts := xceleratconfig.LoadEnrichmentOptions(nil, os.Getenv, xceleratconfig.EnrichmentFlags{})
		if !opts.AnyEnabled() {
			return
		}

		// xcprobe-TODO: swap for PR 594's connection_session.DerivedDataPath()
		// on rebase. Provisional today: the IDE path's manifest lives at
		// …/<DD>/Logs/Build/LogStoreManifest.plist, so grandparent-of-dir is
		// the DerivedData workspace root.
		in := collect.Input{
			DerivedDataPath: provisionalDerivedDataPathFromManifest(manifestPath),
			Scheme:          group.SchemeName(),
			WindowStart:     group.Start(),
			WindowEnd:       group.Stop(),
			HostBase:        host,
			BenchmarkPhase:  benchmarkPhase,
		}

		out := xcprobe.Run(ctx, opts, in, logger)
		if err := xcprobepayload.AttachToInvocation(inv, out); err != nil {
			logger.Warnf("Failed to attach xcprobe enrichment to invocation %s: %v", inv.InvocationID, err)
		}
	}
}

func provisionalDerivedDataPathFromManifest(manifestPath string) string {
	if manifestPath == "" {
		return ""
	}

	logsBuild := filepath.Dir(manifestPath)
	logs := filepath.Dir(logsBuild)

	return filepath.Dir(logs)
}
