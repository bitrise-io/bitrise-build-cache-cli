// Package browse builds the Bitrise dashboard URL that the
// `bitrise-build-cache browse` subcommand opens. Splits cleanly into a
// pure URL builder (url.go, easy to test) and an OS-specific opener
// (open.go).
package browse

import (
	"fmt"
	"net/url"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/consts"
)

// BuildURLParams controls what BuildURL emits.
type BuildURLParams struct {
	WorkspaceID  string
	InvocationID string
	// SourceFilter sets the dashboard's `source` query param ("local" / "ci"). Empty omits.
	SourceFilter string
	// BaseURL overrides consts.BitriseWebsiteBaseURL; empty falls back to the production constant.
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
//
// Workspace + invocation IDs are PathEscape'd defensively — they're slugs
// today, but escape keeps a stray `/` or `#` from breaking the URL.
func BuildURL(p BuildURLParams) (string, error) {
	base := p.BaseURL
	if base == "" {
		base = consts.BitriseWebsiteBaseURL
	}

	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse base URL %q: %w", base, err)
	}

	// Set Path (decoded) + RawPath (encoded) so URL.String uses RawPath
	// verbatim. PathEscape on each segment keeps a stray `/` or `#` in a
	// workspace / invocation slug from splitting or fragmenting the URL.
	ws := url.PathEscape(p.WorkspaceID)
	inv := url.PathEscape(p.InvocationID)

	switch {
	case p.WorkspaceID == "":
		u.Path = "/build-cache"
		u.RawPath = ""

	case p.InvocationID != "":
		u.Path = "/build-cache/" + p.WorkspaceID + "/invocations/" + p.InvocationID
		u.RawPath = "/build-cache/" + ws + "/invocations/" + inv

	default:
		u.Path = "/build-cache/" + p.WorkspaceID + "/invocations"
		u.RawPath = "/build-cache/" + ws + "/invocations"
		if p.SourceFilter != "" {
			q := u.Query()
			q.Set("source", p.SourceFilter)
			u.RawQuery = q.Encode()
		}
	}

	return u.String(), nil
}
