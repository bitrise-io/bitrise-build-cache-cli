package sessions

import (
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

// SidecarSchemaVersion is the on-disk version written by the proxy and read by
// the enrichment side. Readers must leave files with a higher version on disk
// so a newer binary can pick them up — never delete, never attempt to parse.
//
// v3 adds BuildRequestAction and BuildRequestQoS, which the proxy stamps from
// the sidecar's build-request.json so the enricher can classify each sidecar
// (MainBuild / DarkBuild / Indexer / PackageResolve / ...) without reparsing
// the file itself. Pre-v3 sidecars are rejected on read.
const SidecarSchemaVersion = 3

// Sidecar is one line's worth of per-connection accounting written by the
// proxy on connection close (gRPC ConnEnd) or per-conn inactivity.
type Sidecar struct {
	SchemaVersion int    `json:"schema_version"`
	Source        string `json:"source"` // "proxy"
	SidecarUUID   string `json:"sidecar_uuid"`

	PeerPID      int       `json:"peer_pid"`
	PeerAncestry []string  `json:"peer_ancestry"`
	AcceptedAt   time.Time `json:"accepted_at"`
	ClosedAt     time.Time `json:"closed_at"`

	BlobStats *blobstats.Snapshot `json:"blobStats"`

	// WrapperSession carries the derived InvocationID when peer-ancestry anchoring succeeded; nil otherwise.
	WrapperSession *WrapperLink `json:"wrapper_session"`

	// DerivedDataPath is the -derivedDataPath value parsed off the first
	// xcodebuild ancestor at TagConn time. Empty when the flag was not
	// supplied, the ancestor could not be introspected, or no xcodebuild
	// ancestor is present. Additive to schema v2: readers unmarshal into the
	// typed struct and ignore the field when absent — no SchemaVersion bump.
	DerivedDataPath string `json:"derived_data_path,omitempty"`

	// BuildRequestHex is the 32-hex `<hash>.xcbuilddata` stem the IDE handed
	// the proxy at the time this sidecar's stats were accumulated. The reseat
	// flush snapshots the OLD hex before swapBuildRequestHex mutates state so
	// the flushed row keeps the hex that generated its blob stats. Additive to
	// schema v2 like DerivedDataPath; no SchemaVersion bump needed.
	BuildRequestHex string `json:"build_request_hex,omitempty"`

	// BuildStartedAt is the per-build start timestamp stamped when the
	// connection first anchored a derivedInvocationID (or was re-stamped on a
	// ⌘B reseat). Decouples pseudo-group Start from the daemon's AcceptedAt so
	// long-lived SWBBuildService connections don't inflate per-build duration.
	// Additive to schema v2; zero value falls back to AcceptedAt.
	BuildStartedAt time.Time `json:"build_started_at,omitempty"`

	// Terminal reports whether this sidecar closes out the connection
	// (ConnEnd / GracefulStop) or is an intermediate inactivity flush.
	// Additive to schema v2.
	Terminal bool `json:"terminal"`

	// BuildRequestAction is the `action` field read from this sidecar's
	// build-request.json (resolved via DerivedDataPath + BuildRequestHex at
	// write time). The enricher classifies the sidecar's row type from this
	// pair (e.g. "build" + "default" → DarkBuild). Empty when the file could
	// not be read. Added in schema v3.
	BuildRequestAction string `json:"build_request_action,omitempty"`

	// BuildRequestQoS mirrors BuildRequestAction — the build-request.json's
	// qos field. Added in schema v3.
	BuildRequestQoS string `json:"build_request_qos,omitempty"`
}

// WrapperLink carries the derived InvocationID when peer-ancestry anchoring succeeded; nil otherwise.
type WrapperLink struct {
	InvocationID string `json:"invocation_id"`
}
