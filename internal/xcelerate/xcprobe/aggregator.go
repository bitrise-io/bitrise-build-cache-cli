// Package xcprobe is the opt-in enrichment collector tree. Everything
// downstream (collect, payload, host) is config-agnostic; this package owns
// the one hook that reads EnrichmentOptions and runs the enabled-set of
// collectors concurrently.
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

// Run instantiates the enabled Phase-0 collectors, runs each in its own
// goroutine under opts.PerCollectorTimeout, and merges their Outputs into one
// EnrichmentPayload. One collector's failure never kills another; errors are
// captured on EnrichmentPayload.CollectorErrors.
//
// A nil logger is tolerated; errors still surface on CollectorErrors.
func Run(ctx context.Context, opts xceleratconfig.EnrichmentOptions, in collect.Input, logger log.Logger) payload.EnrichmentPayload {
	return runWith(ctx, opts.PerCollectorTimeout, enabledCollectors(opts), in, logger)
}

// runWith is the pure core: given a concrete list of collectors, run them
// under a per-collector timeout and merge outputs into one payload. Exposed
// to the package so tests can inject fakes without wiring real collectors.
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
}
