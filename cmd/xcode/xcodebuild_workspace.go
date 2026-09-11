package xcode

import (
	"context"
	"io"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Matched → replace Config.AuthConfig with the per-workspace Credential.
type workspaceScope struct {
	Slug       string
	Credential authpkg.Credential
	Origin     authpkg.Origin
	// Matched=false leaves the wrapper's Config.AuthConfig untouched.
	Matched bool
}

type workspaceCredResolver func(ctx context.Context, envs map[string]string, workspaceID string) (authpkg.Credential, authpkg.Origin, bool, error)

//nolint:gochecknoglobals
var defaultWorkspaceCredResolver workspaceCredResolver = func(ctx context.Context, envs map[string]string, workspaceID string) (authpkg.Credential, authpkg.Origin, bool, error) {
	return live.Default(nil).ResolveNoRefreshForWorkspace(ctx, envs, workspaceID)
}

func resolveWorkspaceScope(
	ctx context.Context,
	projectDir string,
	envs map[string]string,
	osProxy utils.OsProxy,
	warn io.Writer,
) workspaceScope {
	return resolveWorkspaceScopeWith(ctx, projectDir, envs, osProxy, warn, defaultWorkspaceCredResolver)
}

func resolveWorkspaceScopeWith(
	ctx context.Context,
	projectDir string,
	envs map[string]string,
	osProxy utils.OsProxy,
	warn io.Writer,
	resolver workspaceCredResolver,
) workspaceScope {
	slug := configcommon.DiscoverWorkspaceSlug(projectDir, osProxy, warn)
	if slug == "" {
		return workspaceScope{}
	}

	cred, origin, matched, err := resolver(ctx, envs, slug)
	if err != nil || !matched {
		return workspaceScope{Slug: slug}
	}

	return workspaceScope{Slug: slug, Credential: cred, Origin: origin, Matched: true}
}
