// Package xcprobe runs the enabled set of enrichment collectors concurrently
// and merges their outputs into one EnrichmentPayload.
package xcprobe

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/collect"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

// Run executes the enabled collectors concurrently under
// opts.PerCollectorTimeout and merges their Outputs. One collector's failure
// never kills another; errors surface on EnrichmentPayload.CollectorErrors.
// Nil logger is tolerated. Top-N caps and the attachment-decode gate are
// propagated from opts onto the per-collector Input before dispatch so
// hook sites don't have to re-plumb them.
func Run(ctx context.Context, opts xceleratconfig.EnrichmentOptions, in collect.Input, logger log.Logger) payload.EnrichmentPayload {
	in = applyOptsToInput(opts, in)

	return runWith(ctx, opts.PerCollectorTimeout, enabledCollectors(opts), in, logger)
}

func applyOptsToInput(opts xceleratconfig.EnrichmentOptions, in collect.Input) collect.Input {
	in.DecodeAttachments = opts.ActivityLogAttach

	if in.TargetTopN == 0 {
		in.TargetTopN = opts.TargetTopN
	}

	if in.StepTopN == 0 {
		in.StepTopN = opts.StepTopN
	}

	if in.SwiftTopN == 0 {
		in.SwiftTopN = opts.SwiftTopN
	}

	if in.ClangTopN == 0 {
		in.ClangTopN = opts.ClangTopN
	}

	return in
}

func runWith(ctx context.Context, perTimeout time.Duration, collectors []collect.Collector, in collect.Input, logger log.Logger) payload.EnrichmentPayload {
	out := payload.EnrichmentPayload{SchemaVersion: payload.EnrichmentPayloadSchemaVersion}

	if len(collectors) == 0 {
		return out
	}

	timeout := perTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	type collectorResult struct {
		name   string
		output collect.Output
		err    error
	}

	results := make(chan collectorResult, len(collectors))

	var wg sync.WaitGroup
	for _, c := range collectors {
		wg.Add(1)

		go func(c collect.Collector) {
			defer wg.Done()

			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			defer func() {
				if r := recover(); r != nil {
					results <- collectorResult{name: c.Name(), err: fmt.Errorf("collector panicked: %v", r)}
				}
			}()

			o, err := c.Collect(cctx, in)
			results <- collectorResult{name: c.Name(), output: o, err: err}
		}(c)
	}

	wg.Wait()
	close(results)

	for r := range results {
		if r.err != nil {
			if out.CollectorErrors == nil {
				out.CollectorErrors = map[string]string{}
			}

			out.CollectorErrors[r.name] = r.err.Error()

			if logger != nil {
				logger.Debugf("xcprobe collector %s failed: %v", r.name, r.err)
			}

			continue
		}

		mergeOutput(&out, r.output)
	}

	return out
}

func enabledCollectors(opts xceleratconfig.EnrichmentOptions) []collect.Collector {
	var cs []collect.Collector

	if opts.HostSnapshot {
		cs = append(cs, collect.HostSnapCollector{})
	}

	if opts.BuildTimingSummary {
		cs = append(cs, collect.BuildTimingCollector{})
	}

	if opts.BuildSettings {
		cs = append(cs, collect.BuildSettingsCollector{})
	}

	if opts.ActivityLog || opts.ActivityLogAttach {
		cs = append(cs, collect.ActivityLogCollector{})
	}

	return cs
}

func mergeOutput(dst *payload.EnrichmentPayload, src collect.Output) {
	if src.Host != nil {
		dst.Host = src.Host
	}

	if src.BuildSettings != nil {
		dst.BuildSettings = src.BuildSettings
	}

	if src.BuildTimings != nil {
		dst.BuildTimings = src.BuildTimings
	}

	if len(src.Targets) > 0 {
		dst.Targets = src.Targets
	}

	if len(src.StepTypeHist) > 0 {
		dst.StepTypeHist = src.StepTypeHist
	}

	if len(src.TopSteps) > 0 {
		dst.TopSteps = src.TopSteps
	}

	if len(src.TopSwiftFuncs) > 0 {
		dst.TopSwiftFuncs = src.TopSwiftFuncs
	}

	if len(src.TopSwiftTypes) > 0 {
		dst.TopSwiftTypes = src.TopSwiftTypes
	}

	if src.LinkerStats != nil {
		dst.LinkerStats = src.LinkerStats
	}

	if src.AttachmentsCCM != nil {
		dst.AttachmentsCCM = src.AttachmentsCCM
	}
}
