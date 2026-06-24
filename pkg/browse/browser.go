package browse

import (
	"context"
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/browse"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

// ciProviderUnknown filters the dashboard's invocation list to local runs
// (anything not produced by a recognised CI). Until the FE/BE add a username
// filter this is the closest "show only my local invocations" proxy.
const ciProviderUnknown = "unknown"

var ErrWorkspaceNotConfigured = errors.New(configcommon.EnvWorkspaceID + " not set — pass --workspace, export the env var, or run `bitrise-build-cache auth set` so the dashboard can pick a workspace")

// WorkspaceResolver is called when --workspace + env var have both come up empty.
type WorkspaceResolver func(envs map[string]string) (string, error)

type Params struct {
	WorkspaceID  string
	InvocationID string
	Envs         map[string]string
	BaseURL      string
	PrintOnly    bool
}

type Browser struct {
	Logger            log.Logger
	Opener            browse.Opener
	WorkspaceFromAuth WorkspaceResolver
}

func (b *Browser) Open(ctx context.Context, p Params) (string, error) {
	workspaceID := p.WorkspaceID
	if workspaceID == "" {
		workspaceID = p.Envs[configcommon.EnvWorkspaceID]
	}

	if workspaceID == "" {
		resolver := b.WorkspaceFromAuth
		if resolver == nil {
			resolver = defaultWorkspaceFromAuth
		}

		if id, err := resolver(p.Envs); err == nil && id != "" {
			workspaceID = id
		}
	}

	if workspaceID == "" {
		return "", ErrWorkspaceNotConfigured
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

// defaultWorkspaceFromAuth reads the WorkspaceID from the multiplatform
// analytics config, which every `activate` flow writes.
func defaultWorkspaceFromAuth(_ map[string]string) (string, error) {
	cfg, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	if err != nil {
		return "", err //nolint:wrapcheck // surfaced only as a fallback signal, never propagated to the user
	}

	return cfg.AuthConfig.WorkspaceID, nil
}
