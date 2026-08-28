package bazelconfig

import "strings"

type HostMetadataInventory struct {
	OS             string
	Locale         string
	DefaultCharset string
	CPUCores       int
	MemSize        int64
	Username       string
}

type CommonTemplateInventory struct {
	AuthToken    string
	WorkspaceID  string
	Debug        bool
	AppSlug      string
	CIProvider   string
	RepoURL      string
	WorkflowName string
	BuildID      string
	Timestamps   bool
	// CLIPath is the absolute path of the bitrise-build-cache binary, or the bare
	// binary name when that resolves on $PATH. When set — on CI as well as local
	// dev — it drives `build --credential_helper=<CLIPath>`, so the auth token is
	// resolved per-build via the hidden `get` subcommand (Bazel invokes
	// `<CLIPath> get` per the EngFlow credential-helper spec) instead of being
	// written literally into `~/.bazelrc`. Empty when the CLI is not reachable,
	// which falls back to the literal `Bearer <token>` header.
	CLIPath      string
	HostMetadata HostMetadataInventory
}

type CacheTemplateInventory struct {
	Enabled             bool
	EndpointURLWithPort string
	IsPushEnabled       bool
}

type BESTemplateInventory struct {
	Enabled             bool
	Version             string
	EndpointURLWithPort string
}

type RBETemplateInventory struct {
	Enabled             bool
	EndpointURLWithPort string
}

type TemplateInventory struct {
	Common CommonTemplateInventory
	Cache  CacheTemplateInventory
	BES    BESTemplateInventory
	RBE    RBETemplateInventory
}

// BuildUserHeaderValue is the `x-flare-builduser` header value — the CI provider
// on CI, the resolved display name locally — escaped to sit inside a
// single-quoted Bazel rc value:
//
//	build --remote_header='x-flare-builduser=Jane Doe'
//
// The value has to be quoted in the rc file: Bazel splits rc lines on
// whitespace, so an unquoted display name like `Jane Doe` becomes two
// arguments, and the trailing one is read as a target pattern that fails every
// command with `no such target '//:Doe'`.
//
// Quoting alone is not enough. Inside single quotes Bazel treats a backslash as
// an escape character, so a Windows domain user (`CORP\jdoe`) silently loses the
// separator and an apostrophe (`Pat O'Brien`) closes the quote early and drops
// the rest. Escaping both keeps the value intact.
func (i CommonTemplateInventory) BuildUserHeaderValue() string {
	buildUser := i.CIProvider
	if buildUser == "" {
		buildUser = i.HostMetadata.Username
	}

	return bazelRCEscape(buildUser)
}

// bazelRCEscape escapes a value for use inside a single-quoted Bazel rc value.
// Backslash first, so the backslashes it introduces are not escaped again.
func bazelRCEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)

	return strings.ReplaceAll(value, `'`, `\'`)
}
