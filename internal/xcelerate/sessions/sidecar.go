package sessions

import (
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

// SidecarSchemaVersion is the on-disk version written by the proxy and read by
// the enrichment side. Readers must leave files with a higher version on disk
// so a newer binary can pick them up — never delete, never attempt to parse.
const SidecarSchemaVersion = 2

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
}

// WrapperLink carries the derived InvocationID when peer-ancestry anchoring succeeded; nil otherwise.
type WrapperLink struct {
	InvocationID string `json:"invocation_id"`
}
