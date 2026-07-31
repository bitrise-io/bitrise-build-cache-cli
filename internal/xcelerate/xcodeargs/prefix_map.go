package xcodeargs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// inherited is the Xcode/clang build-setting marker that pulls in the previous
// value from the setting cascade (target/project/xcconfig). Without it we would
// silently drop whatever OTHER_CFLAGS the user's project already had.
const inheritedMarker = "$(inherited)"

// bitriseVirtualPathPrefix marks a prefix-map rule as wrapper-injected. Any
// -fdepscan-prefix-map=<abs>=<virtual> token whose <virtual> starts with this
// prefix is one we (or a parent xcodebuild wrapper invocation) added, and
// must be dropped before splicing the fresh suffix — otherwise nested /
// recursive xcodebuild invocations accumulate stale rules with ambiguous
// precedence.
const bitriseVirtualPathPrefix = "/^"

const prefixMapFlag = "-fdepscan-prefix-map="

// Build-setting keys and derivedDataPath flag literal used by the wrapper
// when injecting prefix-map rules.
const (
	ClangEnablePrefixMappingKey = "CLANG_ENABLE_PREFIX_MAPPING"
	OtherCFlagsKey              = "OTHER_CFLAGS"
	ProjectTempDirKey           = "PROJECT_TEMP_DIR"
	DerivedDataPathFlag         = "-derivedDataPath"
)

// PrefixMapPaths is the set of absolute paths whose values will be rewritten
// to stable virtual names in clang's CAS cache keys via -fdepscan-prefix-map.
// Empty fields produce no rule.
type PrefixMapPaths struct {
	Home            string
	ProjectDir      string
	DerivedDataPath string
	ProjectTempDir  string
}

// BuildOtherCFlagsValue returns the suffix to append after "$(inherited) " in
// OTHER_CFLAGS. Rule ordering is narrowest-first: when one mapped path sits
// inside another (e.g. DerivedDataPath under $HOME), the narrower rule must
// fire first so its virtual name wins regardless of whether clang interprets
// -fdepscan-prefix-map as first-match-wins or longest-match-wins.
func BuildOtherCFlagsValue(p PrefixMapPaths) string {
	type rule struct{ abs, virtual string }
	rules := []rule{
		{p.ProjectTempDir, "/^obj"},
		{p.DerivedDataPath, "/^dd"},
		{p.ProjectDir, "/^src"},
		{p.Home, "/^home"},
	}

	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		if r.abs == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("-fdepscan-prefix-map=%s=%s", r.abs, r.virtual))
	}

	return strings.Join(parts, " ")
}

// MergeOtherCFlagsValue splices the injected prefix-map suffix into a
// user-supplied OTHER_CFLAGS value. Any leading $(inherited) markers in the
// user value are collapsed to a single leading marker so the final layout is:
//
//	$(inherited) <user tokens minus $(inherited)> <suffix>
//
// An empty user value + empty suffix returns "". An empty user value with a
// non-empty suffix still emits $(inherited) so the target/xcconfig chain is
// not lost.
func MergeOtherCFlagsValue(userValue, suffix string) string {
	userTokens := stripInheritedAndBitriseInjected(strings.Fields(userValue))
	suffixTrimmed := strings.TrimSpace(suffix)

	if len(userTokens) == 0 && suffixTrimmed == "" {
		return ""
	}

	parts := []string{inheritedMarker}
	parts = append(parts, userTokens...)
	if suffixTrimmed != "" {
		parts = append(parts, suffixTrimmed)
	}

	return strings.Join(parts, " ")
}

func stripInheritedAndBitriseInjected(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if t == inheritedMarker {
			continue
		}
		if isBitrisePrefixMapRule(t) {
			continue
		}
		out = append(out, t)
	}

	return out
}

// isBitrisePrefixMapRule reports whether an OTHER_CFLAGS token is a
// wrapper-injected prefix-map rule. Matches -fdepscan-prefix-map=<abs>=<virt>
// where <virt> begins with bitriseVirtualPathPrefix ("/^"). User-authored
// prefix-map rules with different virtual namespaces are preserved.
func isBitrisePrefixMapRule(token string) bool {
	rest, ok := strings.CutPrefix(token, prefixMapFlag)
	if !ok {
		return false
	}
	_, virtual, ok := strings.Cut(rest, "=")
	if !ok {
		return false
	}

	return strings.HasPrefix(virtual, bitriseVirtualPathPrefix)
}

// DerivedDataPath returns the last -derivedDataPath value from OriginalArgs,
// supporting both `-derivedDataPath X` and `-derivedDataPath=X` forms. A value
// that begins with `-` in the space form is treated as the next flag and the
// current one as missing. Relative values are resolved against the current
// working directory — clang rejects non-absolute inputs to -fdepscan-prefix-map=.
func (p Default) DerivedDataPath() string {
	return absOrEmpty(findLastFlagValue(p.OriginalArgs, "derivedDataPath"))
}

// ProjectTempDir returns the last PROJECT_TEMP_DIR=... build-setting value
// from OriginalArgs. Missing → empty. Relative values are resolved against the
// current working directory — clang rejects non-absolute inputs to
// -fdepscan-prefix-map=.
func (p Default) ProjectTempDir() string {
	var last string
	for _, arg := range p.OriginalArgs {
		if v, ok := strings.CutPrefix(arg, "PROJECT_TEMP_DIR="); ok {
			last = v
		}
	}

	return absOrEmpty(last)
}

// ProjectDir returns the parent directory of the -project or -workspace value
// on OriginalArgs. -project wins when both are present. When neither flag is
// present xcodebuild auto-discovers a single .xcworkspace/.xcodeproj in CWD,
// so CWD is the project dir. Relative -project/-workspace values are resolved
// against the current working directory before taking the parent — a bare
// `App.xcworkspace` would otherwise give `.`, which clang rejects as a
// -fdepscan-prefix-map= input.
func (p Default) ProjectDir() string {
	if v := findLastFlagValue(p.OriginalArgs, "project"); v != "" {
		return absOrEmpty(filepath.Dir(v))
	}
	if v := findLastFlagValue(p.OriginalArgs, "workspace"); v != "" {
		return absOrEmpty(filepath.Dir(v))
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	return absOrEmpty(cwd)
}

// absOrEmpty resolves a possibly-relative path to an absolute one against the
// current working directory. Empty input passes through unchanged (empty means
// "no rule"). On resolve error the path is dropped rather than emitted as a
// broken relative value.
func absOrEmpty(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}

	return abs
}

// findLastFlagValue scans argv for either `-name value` or `-name=value` (also
// tolerating `--name`). Returns the last value found. Space-form values that
// begin with `-` are rejected as missing (they are the next flag).
func findLastFlagValue(argv []string, name string) string {
	var last string
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		trimmed := strings.TrimLeft(arg, "-")

		if eq := strings.IndexByte(trimmed, '='); eq >= 0 {
			if trimmed[:eq] == name {
				last = trimmed[eq+1:]
			}

			continue
		}

		if trimmed != name {
			continue
		}
		if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "-") {
			continue
		}
		last = argv[i+1]
		i++
	}

	return last
}

// UserOtherCFlags returns the last OTHER_CFLAGS=... build-setting value from
// OriginalArgs. Missing → empty.
func (p Default) UserOtherCFlags() string {
	var last string
	for _, arg := range p.OriginalArgs {
		if v, ok := strings.CutPrefix(arg, OtherCFlagsKey+"="); ok {
			last = v
		}
	}

	return last
}
