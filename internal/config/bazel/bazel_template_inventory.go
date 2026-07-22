package bazelconfig

type HostMetadataInventory struct {
	OS             string
	Locale         string
	DefaultCharset string
	CPUCores       int
	MemSize        int64
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
	// CLIPath is the absolute path of the bitrise-build-cache binary. On local
	// dev (empty CIProvider) it drives `build --credential_helper=<CLIPath>`
	// so the auth token is resolved per-build via the hidden `get` subcommand
	// (Bazel invokes `<CLIPath> get` per the EngFlow credential-helper spec)
	// instead of being written literally into `~/.bazelrc`. Empty on CI —
	// the CI branch keeps the literal `Bearer <token>` header for perf.
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
