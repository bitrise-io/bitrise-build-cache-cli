package proxy

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildidentity"
)

// AncestryEntry is proxy's local alias for buildidentity.AncestryEntry so
// existing proxy call sites keep working without an import rename.
type AncestryEntry = buildidentity.AncestryEntry

// toNames projects the names-only ancestry chain written into the sidecar's
// PeerAncestry field. Reads from entries already captured at TagConn time so
// the peer PID's exit between accept and ConnEnd cannot blank out the chain.
func toNames(entries []AncestryEntry) []string {
	if len(entries) == 0 {
		return nil
	}

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}

	return names
}
