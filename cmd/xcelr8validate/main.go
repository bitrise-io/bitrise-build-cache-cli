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
	"sort"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/invocations"
)

type listFile struct {
	Items  []json.RawMessage `json:"items"`
	Paging map[string]any    `json:"paging"`
}

type fileResult struct {
	path      string
	itemCount int
	errCount  int
	rawSeen   map[string]int
}

func processFile(path string) (fileResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileResult{}, fmt.Errorf("read %s: %w", path, err)
	}

	var lf listFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return fileResult{}, fmt.Errorf("unmarshal %s: %w", path, err)
	}

	fmt.Fprintf(os.Stdout, "=== %s — %d items ===\n", path, len(lf.Items))

	res := fileResult{path: path, itemCount: len(lf.Items), rawSeen: map[string]int{}}

	for i, raw := range lf.Items {
		var inv invocations.InvocationSummary
		if err := json.Unmarshal(raw, &inv); err != nil {
			res.errCount++
			if res.errCount <= 3 {
				fmt.Fprintf(os.Stdout, "  [%d] DECODE ERROR: %v\n", i, err)
				fmt.Fprintf(os.Stdout, "       raw[:200]: %s\n", clip(string(raw), 200))
			}

			continue
		}

		var bag map[string]any
		if err := json.Unmarshal(raw, &bag); err != nil {
			continue
		}
		for k, v := range bag {
			if v != nil && v != "" {
				res.rawSeen[k]++
			}
		}
	}

	if res.errCount == 0 {
		fmt.Fprintf(os.Stdout, "  ✓ all %d items decoded cleanly through InvocationSummary\n", len(lf.Items))
	} else {
		fmt.Fprintf(os.Stdout, "  ✗ %d / %d items failed to decode\n", res.errCount, len(lf.Items))
	}

	return res, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: xcelr8validate <list-tool.json> [more.json …]\n")
		os.Exit(2)
	}

	totalItems, totalDecodeErrs := 0, 0
	rawSeen := map[string]int{}
	rawTotal := 0

	for _, path := range os.Args[1:] {
		res, err := processFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}

		totalItems += res.itemCount
		totalDecodeErrs += res.errCount
		rawTotal += res.itemCount
		for k, v := range res.rawSeen {
			rawSeen[k] += v
		}
	}

	fmt.Fprintf(os.Stdout, "\n=== summary ===\nitems: %d\ndecode errors: %d\n\n", totalItems, totalDecodeErrs)

	fmt.Fprintf(os.Stdout, "field population (n=%d):\n", rawTotal)

	keys := make([]string, 0, len(rawSeen))
	for k := range rawSeen {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if rawSeen[a] != rawSeen[b] {
			return rawSeen[a] > rawSeen[b]
		}

		return a < b
	})

	for _, k := range keys {
		c := rawSeen[k]
		var marker string
		switch c {
		case rawTotal:
			marker = "✓ "
		case 0:
			marker = "  "
		default:
			marker = "~ "
		}

		fmt.Fprintf(os.Stdout, "  %s%-32s %d / %d\n", marker, k, c, rawTotal)
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
