package browse

import (
	"context"
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/browse"
)

const WorkspaceIDEnvVar = "BITRISE_BUILD_CACHE_WORKSPACE_ID"

var ErrWorkspaceNotConfigured = errors.New(WorkspaceIDEnvVar + " not set — pass --workspace or export the env var to open the dashboard for a specific workspace")

type Params struct {
	WorkspaceID  string
	InvocationID string
	Envs         map[string]string
	BaseURL      string
	PrintOnly    bool
}

type Browser struct {
	Logger log.Logger
	Opener browse.Opener
}

func (b *Browser) Open(ctx context.Context, p Params) (string, error) {
	workspaceID := p.WorkspaceID
	if workspaceID == "" {
		workspaceID = p.Envs[WorkspaceIDEnvVar]
	}

	if workspaceID == "" {
		return "", ErrWorkspaceNotConfigured
	}

	source := ""
	if p.InvocationID == "" {
		source = "local"
	}

	dashboardURL, err := browse.BuildURL(browse.BuildURLParams{
		WorkspaceID:  workspaceID,
		InvocationID: p.InvocationID,
		SourceFilter: source,
		BaseURL:      p.BaseURL,
	})
	if err != nil {
		return "", fmt.Errorf("build dashboard URL: %w", err)
	}

	if b.Logger != nil {
		b.Logger.Infof("Bitrise Build Cache dashboard: %s", dashboardURL)
	}

	if p.PrintOnly {
		return dashboardURL, nil
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

	return dashboardURL, nil
}
