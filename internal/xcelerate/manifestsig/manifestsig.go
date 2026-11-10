// Package manifestsig parses the tokens Xcode writes into
// LogStoreManifest.plist entries' Signature field. The signature follows a
// loose format like "Build workspace Foo with scheme Bar Configuration Debug"
// or "Building project Baz with scheme Qux"; both the wrapperless and
// enrichment flows parse the same tokens, so the regexes + helper live here
// to keep them from drifting.
package manifestsig

import "regexp"

//nolint:gochecknoglobals // compiled once, read-only
var (
	// Project matches "project <Foo>" (case-insensitive), capturing <Foo>.
	Project = regexp.MustCompile(`(?i)\bproject\s+(\S+)`)

	// Workspace matches "workspace <Foo>" (case-insensitive), capturing <Foo>.
	// Workspace-form signatures from `xcodebuild -workspace` carry this
	// token in place of the project / configuration tokens.
	Workspace = regexp.MustCompile(`(?i)\bworkspace\s+(\S+)`)

	// Configuration matches "configuration <Foo>" (case-insensitive),
	// capturing <Foo>.
	Configuration = regexp.MustCompile(`(?i)\bconfiguration\s+(\S+)`)
)

// FirstSubmatch runs re against s and returns the first captured group, or
// "" when no match or the capture is absent.
func FirstSubmatch(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); len(m) == 2 {
		return m[1]
	}

	return ""
}
