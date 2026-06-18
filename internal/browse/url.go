package browse

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/consts"
)

// ErrMissingWorkspace is returned by BuildURL when WorkspaceID is empty.
// Callers should resolve the workspace upstream — `pkg/browse` does so via
// the --workspace flag, the env var, and the configured auth config.
var ErrMissingWorkspace = errors.New("BuildURL: WorkspaceID is required")

type BuildURLParams struct {
	WorkspaceID      string
	InvocationID     string
	CIProviderFilter string
	BaseURL          string
}

func BuildURL(p BuildURLParams) (string, error) {
	if p.WorkspaceID == "" {
		return "", ErrMissingWorkspace
	}

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

	if p.InvocationID != "" {
		u.Path = "/build-cache/" + p.WorkspaceID + "/invocations/" + p.InvocationID
		u.RawPath = "/build-cache/" + ws + "/invocations/" + inv

		return u.String(), nil
	}

	u.Path = "/build-cache/" + p.WorkspaceID + "/invocations"
	u.RawPath = "/build-cache/" + ws + "/invocations"

	if p.CIProviderFilter != "" {
		q := u.Query()
		q.Set("ci_provider", p.CIProviderFilter)
		u.RawQuery = q.Encode()
	}

	return u.String(), nil
}
