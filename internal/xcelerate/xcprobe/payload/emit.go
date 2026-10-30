package payload

import (
	"encoding/json"
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

// SoftCapBytes is the enrichment payload size the attach step tries to stay
// under. If the first marshal exceeds it, the attach step drops the largest
// sub-row and remarshals once; it never fails solely on size.
const SoftCapBytes = 64 * 1024

// AttachToInvocation marshals p and writes it to inv.Enrichment as
// json.RawMessage. If the first marshal exceeds SoftCapBytes, the largest
// sub-row (host / buildSettings / buildTimings by serialised length) is
// dropped and the attach is retried once; the drop is recorded on
// CollectorErrors so the operator sees why a field vanished. Returns an
// error only if marshaling fails outright.
func AttachToInvocation(inv *analytics.Invocation, p EnrichmentPayload) error {
	if inv == nil {
		return fmt.Errorf("attach enrichment: nil invocation")
	}

	raw, err := json.Marshal(p)
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

		raw, err = json.Marshal(p)
		if err != nil {
			return fmt.Errorf("re-marshal enrichment payload after soft-cap drop: %w", err)
		}
	}

	inv.Enrichment = raw

	return nil
}

// dropLargestSubRow nils out whichever of host/buildSettings/buildTimings
// serialises to the most bytes and returns the dropped field name. Returns
// "" when every sub-row is already nil. Marshal errors here are impossible
// for the Phase-0 payload shape (plain structs with concrete types), but we
// treat them as size=0 defensively.
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
