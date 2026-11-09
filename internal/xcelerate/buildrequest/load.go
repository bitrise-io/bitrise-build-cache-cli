package buildrequest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Info holds the fields the enrichment classifier reads off a build-request.json.
// `action` is the Xcode build action verb ("build", "indexbuild",
// "resolvePackages", "cleanBuildFolder", "archive", "test", ...).
// `qos` is the build-request QoS string Xcode writes alongside — "default" for
// dark builds, "prepareForIndexing" for indexer, absent for the main build.
// `command` is the free-form command shortcut Xcode writes for display.
type Info struct {
	Action  string
	QoS     string
	Command string
}

// ErrNotFound is returned when neither the Build nor Index.noindex subtree
// yields a build-request.json for the supplied hex.
var ErrNotFound = errors.New("build-request.json not found")

// Load reads <ddPath>/<Build|Index.noindex/Build>/Intermediates.noindex/
// XCBuildData/<hex>.xcbuilddata/build-request.json and returns the parsed
// action/qos/command. Both subtrees are tried; the first that opens wins.
//
// The JSON has evolved across Xcode versions; several jq-equivalent paths are
// probed per field (.parameters.action, .action, .buildRequest.action, ...).
// Soft-fail: a missing file maps to ErrNotFound; a parse failure maps to the
// decode error. Callers decide whether to warn or Debug.
func Load(ddPath, hex string) (Info, error) {
	if ddPath == "" || hex == "" {
		return Info{}, fmt.Errorf("load build-request: empty %q/%q", ddPath, hex)
	}

	candidates := []string{
		filepath.Join(ddPath, "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata", "build-request.json"),
		filepath.Join(ddPath, "Index.noindex", "Build", "Intermediates.noindex", "XCBuildData", hex+".xcbuilddata", "build-request.json"),
	}

	var (
		body       []byte
		pathFound  string
		openErrors []error
	)
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err != nil {
			openErrors = append(openErrors, err)

			continue
		}

		body = b
		pathFound = p

		break
	}

	if pathFound == "" {
		for _, err := range openErrors {
			if os.IsNotExist(err) {
				continue
			}

			return Info{}, fmt.Errorf("read build-request %s: %w", candidates[0], err)
		}

		return Info{}, ErrNotFound
	}

	// Xcode has rewritten this file across versions. Each relevant field lives
	// at one of a short list of jq paths; we try them in order and take the
	// first non-empty value.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return Info{}, fmt.Errorf("decode build-request %s: %w", pathFound, err)
	}

	info := Info{
		Action:  pickStringPath(raw, []string{"parameters.action", "action", "buildRequest.action"}),
		QoS:     pickStringPath(raw, []string{"parameters.qos", "qos", "buildRequest.qos"}),
		Command: pickStringPath(raw, []string{"parameters.command", "command", "buildRequest.command"}),
	}

	return info, nil
}

// pickStringPath walks dotted paths through nested JSON objects and returns
// the first non-empty string value. Caller passes paths in priority order.
func pickStringPath(root map[string]json.RawMessage, candidates []string) string {
	for _, path := range candidates {
		if v := traversePath(root, path); v != "" {
			return v
		}
	}

	return ""
}

func traversePath(root map[string]json.RawMessage, dotted string) string {
	parts := splitDotted(dotted)
	if len(parts) == 0 {
		return ""
	}

	current := root
	for i, key := range parts {
		raw, ok := current[key]
		if !ok {
			return ""
		}

		if i == len(parts)-1 {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return ""
			}

			return s
		}

		var next map[string]json.RawMessage
		if err := json.Unmarshal(raw, &next); err != nil {
			return ""
		}

		current = next
	}

	return ""
}

func splitDotted(s string) []string {
	if s == "" {
		return nil
	}

	out := make([]string, 0, 2)
	start := 0
	for i := range len(s) {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])

	return out
}
