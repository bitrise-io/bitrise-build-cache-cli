package bazelconfig

type HostMetadataInventory struct {
	OS             string
	Locale         string
	DefaultCharset string
	CPUCores       string
	MemSize        string
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
