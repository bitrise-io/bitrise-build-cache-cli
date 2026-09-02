package browse

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/browse"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// ciProviderUnknown filters the dashboard's invocation list to local runs
// (anything not produced by a recognised CI). Until the FE/BE add a username
// filter this is the closest "show only my local invocations" proxy.
const ciProviderUnknown = "unknown"

var ErrWorkspaceNotConfigured = errors.New(auth.EnvWorkspaceID + " not set — pass --workspace, export the env var, or run `bitrise-build-cache auth set` so the dashboard can pick a workspace")

// ErrAmbiguousWorkspace is returned when the auth store holds credentials for
// more than one workspace and the caller has neither passed --workspace nor
// dropped a project marker. The error string enumerates the available slugs.
var ErrAmbiguousWorkspace = errors.New("multiple workspaces have stored credentials — specify --workspace <slug>")

// WorkspaceResolver is called when --workspace + env var have both come up empty.
type WorkspaceResolver func(envs map[string]string) (string, error)

type Params struct {
	WorkspaceID  string
	InvocationID string
	Envs         map[string]string
	BaseURL      string
	PrintOnly    bool
}

type Result struct {
	URL          string `json:"url"`
	WorkspaceID  string `json:"workspace_id"`
	InvocationID string `json:"invocation_id,omitempty"`
}

type Browser struct {
	Logger            log.Logger
	Opener            browse.Opener
	WorkspaceFromAuth WorkspaceResolver
}

func (b *Browser) Open(ctx context.Context, p Params) (Result, error) {
	workspaceID := p.WorkspaceID
	if workspaceID == "" {
		workspaceID = p.Envs[auth.EnvWorkspaceID]
	}

	if workspaceID == "" {
		resolver := b.WorkspaceFromAuth
		if resolver == nil {
			resolver = defaultWorkspaceFromAuth
		}

		id, err := resolver(p.Envs)
		if errors.Is(err, ErrAmbiguousWorkspace) {
			return Result{}, err
		}
		if err == nil && id != "" {
			workspaceID = id
		}
	}

	if workspaceID == "" {
		return Result{}, ErrWorkspaceNotConfigured
	}

	ciProvider := ""
	if p.InvocationID == "" {
		ciProvider = ciProviderUnknown
	}

	dashboardURL, err := browse.BuildURL(browse.BuildURLParams{
		WorkspaceID:      workspaceID,
		InvocationID:     p.InvocationID,
		CIProviderFilter: ciProvider,
		BaseURL:          p.BaseURL,
	})
	if err != nil {
		return Result{}, fmt.Errorf("build dashboard URL: %w", err)
	}

	res := Result{URL: dashboardURL, WorkspaceID: workspaceID, InvocationID: p.InvocationID}

	if b.Logger != nil {
		b.Logger.Infof("Bitrise Build Cache dashboard: %s", dashboardURL)
	}

	if p.PrintOnly {
		return res, nil
	}

	opener := b.Opener
	if opener == nil {
		opener = browse.DefaultOpener{}
	}

	if err := opener.Open(ctx, dashboardURL); err != nil {
		if b.Logger != nil {
			switch {
			case errors.Is(err, browse.ErrNoOpener):
				b.Logger.Warnf("No default browser launcher for this OS. Copy the URL above to open it manually.")
			default:
				b.Logger.Warnf("Could not auto-launch the browser (%v). Copy the URL above to open it manually.", err)
			}
		}
	}

	return res, nil
}

// authResolver is the resolver seam. Tests satisfy it in-memory; production uses
// the live.Default resolver via defaultWorkspaceFromAuth.
type authResolver interface {
	ResolveNoRefresh(envs map[string]string) (auth.Credential, auth.Origin, bool, error)
	StoredWorkspaceSlugs() []string
}

func defaultWorkspaceFromAuth(envs map[string]string) (string, error) {
	return workspaceFromAuth(live.Default(nil), envs)
}

func workspaceFromAuth(resolver authResolver, envs map[string]string) (string, error) {
	cfg, _, workspacesOnly, err := resolver.ResolveNoRefresh(envs)
	if err != nil {
		return "", err //nolint:wrapcheck // surfaced only as a fallback signal, never propagated to the user
	}

	if !workspacesOnly {
		return cfg.WorkspaceID, nil
	}

	// Marker in or above CWD wins: it's the explicit per-project choice.
	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		if _, marker, mErr := configcommon.WalkUpFindMarker(cwd, utils.DefaultOsProxy{}); mErr == nil && marker != nil && marker.Workspace != "" {
			return marker.Workspace, nil
		}
	}

	// No marker: a single stored workspace is the unambiguous default. More than
	// one and we can't pick for the user; surface the ambiguity with the choices.
	slugs := resolver.StoredWorkspaceSlugs()
	switch len(slugs) {
	case 0:
		return "", nil //nolint:nilerr // no marker + no workspace entries: caller renders "no workspace configured"
	case 1:
		return slugs[0], nil
	default:
		return "", fmt.Errorf("%w (available: %s)", ErrAmbiguousWorkspace, strings.Join(slugs, ", "))
	}
}
