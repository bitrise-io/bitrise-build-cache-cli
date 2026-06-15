// Package browse builds the Bitrise dashboard URL that the
// `bitrise-build-cache browse` subcommand opens. Splits cleanly into a
// pure URL builder (url.go, easy to test) and an OS-specific opener
// (open.go).
package browse

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/consts"
)

// BuildURLParams controls what BuildURL emits. WorkspaceID is required;
// everything else is optional.
type BuildURLParams struct {
	// WorkspaceID is the Bitrise workspace slug — required for any
	// per-workspace dashboard URL. Empty workspace falls back to the
	// generic /build-cache landing page, useful as a graceful-degrade
	// behaviour when the user hasn't configured auth yet.
	WorkspaceID string

	// InvocationID, when non-empty, deep-links to the specific invocation
	// page. Combined with WorkspaceID it produces a /build-cache/<ws>/
	// invocations/<id> URL. Without an InvocationID the URL points at the
	// per-workspace invocations list.
	InvocationID string

	// SourceFilter is the value of the dashboard's `source` query param,
	// today either "local" or "ci". Empty omits the param. The dashboard
	// currently ignores unknown values gracefully — sending it now is
	// harmless and forward-compatible with F4 / ACI-5047 (BE accepts the
	// field).
	SourceFilter string

	// BaseURL overrides the dashboard host. Tests use this to assert URLs
	// against a fixture host without touching the production constant.
	// Empty falls back to consts.BitriseWebsiteBaseURL.
	BaseURL string
}

// BuildURL returns the dashboard URL the browse subcommand should open.
//
// Examples (with WorkspaceID="ws_abc"):
//
//	BuildURL(WorkspaceID="ws_abc")
//	 → https://app.bitrise.io/build-cache/ws_abc/invocations?source=local
//
//	BuildURL(WorkspaceID="ws_abc", InvocationID="inv_xyz")
//	 → https://app.bitrise.io/build-cache/ws_abc/invocations/inv_xyz
//
//	BuildURL(WorkspaceID="")
//	 → https://app.bitrise.io/build-cache
func BuildURL(p BuildURLParams) (string, error) {
	base := p.BaseURL
	if base == "" {
		base = consts.BitriseWebsiteBaseURL
	}

	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse base URL %q: %w", base, err)
	}

	switch {
	case p.WorkspaceID == "":
		u.Path = "/build-cache"

	case p.InvocationID != "":
		u.Path = "/build-cache/" + p.WorkspaceID + "/invocations/" + p.InvocationID

	default:
		u.Path = "/build-cache/" + p.WorkspaceID + "/invocations"
		if p.SourceFilter != "" {
			q := u.Query()
			q.Set("source", p.SourceFilter)
			u.RawQuery = q.Encode()
		}
	}

	return strings.TrimSuffix(u.String(), "?"), nil
}
