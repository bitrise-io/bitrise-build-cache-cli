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

// ErrWorkspaceNotConfigured is returned when no workspace ID is supplied
// either explicitly (Params.WorkspaceID) or via the env-var fallback.
// Browse doesn't need an auth token (the URL is opened in the user's
// browser, which handles its own session), so this is distinct from
// common.ErrWorkspaceIDNotProvided which couples workspace + token.
var ErrWorkspaceNotConfigured = errors.New(WorkspaceIDEnvVar + " not set — pass --workspace or export the env var to open the dashboard for a specific workspace")

// Params controls Browse behaviour.
type Params struct {
	WorkspaceID  string
	InvocationID string
	Envs         map[string]string
	// BaseURL overrides consts.BitriseWebsiteBaseURL. Set in tests / when
	// pointing at a staging dashboard. Empty falls back to production.
	BaseURL string
	// PrintOnly suppresses the auto-open step. The URL is still logged at
	// Info level either way.
	PrintOnly bool
}

type Browser struct {
	Logger log.Logger
	Opener browse.Opener
}

// Open builds the dashboard URL and launches the user's default browser.
// The URL is always logged at Info level, so PrintOnly + launcher errors +
// no-recognised-launcher all degrade to "URL is in the log; copy it".
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
