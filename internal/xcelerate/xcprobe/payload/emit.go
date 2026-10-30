package payload

import (
	"encoding/json"
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

// SoftCapBytes is the target payload size. Over the cap, the largest sub-row
// is dropped and marshal retried once; attach never fails solely on size.
const SoftCapBytes = 64 * 1024

// AttachToInvocation marshals p into inv.Enrichment. When the first marshal
// exceeds SoftCapBytes, the largest of host / buildSettings / buildTimings
// (by serialised length) is dropped, the drop recorded on CollectorErrors,
// and the attach retried once. At most one sub-row is dropped per call.
func AttachToInvocation(inv *analytics.Invocation, p EnrichmentPayload) error {
	if inv == nil {
		return fmt.Errorf("attach enrichment: nil invocation")
	}

	// common.HostMetadata (transitively embedded) has no json tags by design.
	raw, err := json.Marshal(p) //nolint:musttag
	if err != nil {
		return fmt.Errorf("marshal enrichment payload: %w", err)
	}

	if len(raw) > SoftCapBytes {
		dropped := dropLargestSubRow(&p)
		if dropped != "" {
			if p.CollectorErrors == nil {
				p.CollectorErrors = map[string]string{}
			}

			p.CollectorErrors[dropped] = fmt.Sprintf("dropped: payload exceeded %d bytes", SoftCapBytes)
		}

		raw, err = json.Marshal(p) //nolint:musttag
		if err != nil {
			return fmt.Errorf("re-marshal enrichment payload after soft-cap drop: %w", err)
		}
	}

	inv.Enrichment = raw

	return nil
}

// dropLargestSubRow nils out whichever of host/buildSettings/buildTimings
// serialises to the most bytes and returns its name, "" when all are nil.
func dropLargestSubRow(p *EnrichmentPayload) string {
	type candidate struct {
		name  string
		size  int
		clear func()
	}

	candidates := make([]candidate, 0, 3)

	if p.Host != nil {
		candidates = append(candidates, candidate{name: "host", size: marshalSize(p.Host), clear: func() { p.Host = nil }})
	}

	if p.BuildSettings != nil {
		candidates = append(candidates, candidate{name: "buildSettings", size: marshalSize(p.BuildSettings), clear: func() { p.BuildSettings = nil }})
	}

	if p.BuildTimings != nil {
		candidates = append(candidates, candidate{name: "buildTimings", size: marshalSize(p.BuildTimings), clear: func() { p.BuildTimings = nil }})
	}

	if len(candidates) == 0 {
		return ""
	}

	biggest := candidates[0]
	for _, c := range candidates[1:] {
		if c.size > biggest.size {
			biggest = c
		}
	}

	biggest.clear()

	return biggest.name
}

func marshalSize(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}

	return len(b)
}
