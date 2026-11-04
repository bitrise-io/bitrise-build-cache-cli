package bazelconfig

import (
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
)

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
	// StagingServices and WebsiteBaseURL are empty for hand-built inventories, which render production values.
	StagingServices bool
	WebsiteBaseURL  string
}

// CredentialHostPatterns lists both domains on staging, as the cache and RBE defaults stay on bitrise.io.
func (i CommonTemplateInventory) CredentialHostPatterns() []string {
	if i.StagingServices {
		return []string{"*.services.bitrise.io", "*.services.bitrise.dev"}
	}

	return []string{"*.services.bitrise.io"}
}

func (i CommonTemplateInventory) BESResultsURL() string {
	base := i.WebsiteBaseURL
	if base == "" {
		base = consts.BitriseWebsiteBaseURL
	}

	return base + "/build-cache/invocations/bazel/"
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

func (i CommonTemplateInventory) BuildUserHeaderValue() string {
	buildUser := i.CIProvider
	if buildUser == "" {
		buildUser = i.HostMetadata.Username
	}

	return bazelRCEscape(buildUser)
}

func (i CommonTemplateInventory) WorkflowNameHeaderValue() string {
	return bazelRCEscape(i.WorkflowName)
}

// bazelRCEscape escapes a value for use inside a single-quoted Bazel rc value.
// Backslash first, so the backslashes it introduces are not escaped again.
func bazelRCEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)

	return strings.ReplaceAll(value, `'`, `\'`)
}
