package live

import (
	"fmt"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

// Pure formatting: Origin already carries the provenance, so nothing is re-read
// from disk.
func Describe(cred auth.Credential, origin auth.Origin) string {
	out := origin.Label()

	// A JWT embeds the workspace in the token; surfacing it adds nothing.
	if cred.WorkspaceID != "" && origin.Backend != auth.BackendJWT {
		out += fmt.Sprintf(" (workspace %s)", cred.WorkspaceID)
	}

	switch {
	case cred.Expiry.IsZero():
	case cred.Expired():
		out += ", token expired — refreshes on next use"
	default:
		out += ", token valid until " + cred.Expiry.Format(time.RFC3339)
	}

	return out
}

// DescribePendingBroker is Describe for a VM token exchange that has not happened yet.
func DescribePendingBroker() string {
	return auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceBrokered}.Label() +
		", exchanged when a command first needs a credential"
}
