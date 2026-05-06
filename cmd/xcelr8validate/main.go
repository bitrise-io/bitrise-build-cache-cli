// xcelr8validate decodes prod invocation snapshots through
// `internal/invocations.InvocationSummary` to detect drift between the Go
// struct and the live `BuildToolInvocationInfoPresenter#to_h` shape.
//
// ACI-4914 verification harness.
//
// Usage:
//
//	go run ./cmd/xcelr8validate _xcelr8/snapshots/list-xcode.json [list-gradle.json …]
//
// For each file it reports decode errors, then prints a summary of which
// canonical fields were observed populated vs. always-empty across the
// decoded set — fast feedback for any field mismatch.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/invocations"
)

type listFile struct {
	Items  []json.RawMessage      `json:"items"`
	Paging map[string]any         `json:"paging"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: xcelr8validate <list-tool.json> [more.json …]\n")
		os.Exit(2)
	}

	totalItems, totalDecodeErrs := 0, 0

	// Track per-field "ever populated" so we can surface fields the Go
	// struct claims always-present but never see populated, and conditionals
	// that turned out to be effectively required.
	rawSeen := map[string]int{}
	rawTotal := 0

	for _, path := range os.Args[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: read: %v\n", path, err)
			os.Exit(1)
		}

		var lf listFile
		if err := json.Unmarshal(data, &lf); err != nil {
			fmt.Fprintf(os.Stderr, "%s: unmarshal envelope: %v\n", path, err)
			os.Exit(1)
		}

		fmt.Printf("=== %s — %d items ===\n", path, len(lf.Items))
		fileErrs := 0

		for i, raw := range lf.Items {
			totalItems++
			rawTotal++

			// Strict decode through the Go struct.
			var inv invocations.InvocationSummary
			if err := json.Unmarshal(raw, &inv); err != nil {
				totalDecodeErrs++
				fileErrs++

				if fileErrs <= 3 {
					fmt.Printf("  [%d] DECODE ERROR: %v\n", i, err)
					fmt.Printf("       raw[:200]: %s\n", clip(string(raw), 200))
				}

				continue
			}

			// Loose decode to surface every key the wire actually sent.
			var bag map[string]any
			if err := json.Unmarshal(raw, &bag); err != nil {
				continue
			}
			for k, v := range bag {
				if v != nil && v != "" {
					rawSeen[k]++
				}
			}
		}

		if fileErrs == 0 {
			fmt.Printf("  ✓ all %d items decoded cleanly through InvocationSummary\n", len(lf.Items))
		} else {
			fmt.Printf("  ✗ %d / %d items failed to decode\n", fileErrs, len(lf.Items))
		}
	}

	fmt.Printf("\n=== summary ===\nitems: %d\ndecode errors: %d\n\n", totalItems, totalDecodeErrs)

	// Field-population summary — useful for catching fields we model as
	// always-present that prod actually omits.
	fmt.Printf("field population (n=%d):\n", rawTotal)

	keys := make([]string, 0, len(rawSeen))
	for k := range rawSeen {
		keys = append(keys, k)
	}
	// Stable sort by frequency desc, then name.
	sortByFreqThenName(keys, rawSeen)

	for _, k := range keys {
		c := rawSeen[k]
		marker := "  "
		switch {
		case c == rawTotal:
			marker = "✓ "
		case c == 0:
			marker = "  "
		default:
			marker = "~ "
		}

		fmt.Printf("  %s%-32s %d / %d\n", marker, k, c, rawTotal)
	}

	if totalDecodeErrs > 0 {
		os.Exit(1)
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "…"
}

func sortByFreqThenName(keys []string, freq map[string]int) {
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			a, b := keys[i], keys[j]
			if freq[a] < freq[b] || (freq[a] == freq[b] && a > b) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
}
