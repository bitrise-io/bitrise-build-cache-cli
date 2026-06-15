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

// WorkspaceIDEnvVar is the env-var name browse reads for the workspace
// slug fallback. Kept here as the canonical source so docs + tests can
// reference it without duplicating the literal.
const WorkspaceIDEnvVar = "BITRISE_BUILD_CACHE_WORKSPACE_ID"

// ErrWorkspaceNotConfigured is returned when no workspace ID is supplied
// either explicitly (Params.WorkspaceID) or via the env-var fallback.
// browse doesn't need an auth token (the URL is opened in the user's
// browser, which handles its own session), so this is a distinct error
// from common.ErrWorkspaceIDNotProvided which couples workspace + token.
var ErrWorkspaceNotConfigured = errors.New(WorkspaceIDEnvVar + " not set — pass --workspace or export the env var to open the dashboard for a specific workspace")

// Params controls Browse behaviour. WorkspaceID is the only required field
// — Envs is consulted as a fallback when it's empty.
type Params struct {
	// WorkspaceID overrides the workspace slug. Empty falls back to
	// reading BITRISE_BUILD_CACHE_WORKSPACE_ID from Envs.
	WorkspaceID string
	// InvocationID, when non-empty, deep-links to a specific invocation
	// page instead of the per-workspace list.
	InvocationID string
	// Envs is the environment snapshot the workspace fallback reads from.
	// Production callers pass utils.AllEnvs().
	Envs map[string]string
	// BaseURL overrides the dashboard host. Empty falls back to the
	// production constant. Used by tests + by anyone wanting to point at a
	// staging dashboard.
	BaseURL string
	// PrintOnly skips the launcher and only logs the URL. Useful in
	// headless environments / CI / when --print is passed at the cobra
	// layer.
	PrintOnly bool
}

// Browser composes the URL builder + opener. Logger is required so we can
// surface the URL when no GUI browser is available (CI / SSH session).
type Browser struct {
	Logger log.Logger
	Opener browse.Opener
}

// Open builds the dashboard URL and launches the user's default browser.
// Falls back to printing the URL on platforms without a recognised
// launcher, on PrintOnly=true, or when the launcher itself errors. The URL
// is always logged at Info level so the user can copy/paste even when the
// browser auto-launched.
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
		// Source filter applies to the list page only; deep links to a
		// specific invocation don't need it. Hard-coded to "local" today
		// because `browse` is positioned as the "open my local
		// invocations" command. Once F4 (BE accepts the field) lands the
		// dashboard will honour this filter.
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
		// Print-fallback rather than hard error — having no GUI browser
		// shouldn't make the command fail. The URL has already been
		// logged above; we just emit a hint.
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
