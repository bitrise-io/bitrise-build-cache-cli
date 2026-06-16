package browse

import (
	"fmt"
	"net/url"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/consts"
)

type BuildURLParams struct {
	WorkspaceID  string
	InvocationID string
	SourceFilter string
	BaseURL      string
}

func BuildURL(p BuildURLParams) (string, error) {
	base := p.BaseURL
	if base == "" {
		base = consts.BitriseWebsiteBaseURL
	}

	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse base URL %q: %w", base, err)
	}

	// PathEscape per segment + RawPath so a stray `/` or `#` in a workspace/invocation slug can't break the URL.
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
