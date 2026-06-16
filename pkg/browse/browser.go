// Package browse exposes the public API for the `bitrise-build-cache
// browse` subcommand. External Go consumers (Bitrise steps, custom
// tooling) construct a Browser and call Open without depending on cobra.
package browse

import (
	"context"
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/browse"
)

const WorkspaceIDEnvVar = "BITRISE_BUILD_CACHE_WORKSPACE_ID"

// ErrWorkspaceNotConfigured is returned when no workspace ID is supplied via Params or env-var.
var ErrWorkspaceNotConfigured = errors.New(WorkspaceIDEnvVar + " not set — pass --workspace or export the env var to open the dashboard for a specific workspace")

// Params controls Browse behaviour.
type Params struct {
	WorkspaceID  string
	InvocationID string
	Envs         map[string]string
	// BaseURL overrides consts.BitriseWebsiteBaseURL; empty falls back to production.
	BaseURL string
	// PrintOnly suppresses the auto-open step (URL is still logged).
	PrintOnly bool
}

type Browser struct {
	Logger log.Logger
	Opener browse.Opener
}

// Open builds the dashboard URL, logs it, and launches the user's default browser.
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
		// `browse` opens the user's local invocations — pin the dashboard filter to match.
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
		// No GUI browser shouldn't fail the command — URL is already logged.
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
