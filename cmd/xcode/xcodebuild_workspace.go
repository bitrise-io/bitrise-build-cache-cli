package xcode

import (
	"context"
	"io"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// workspaceScope is the marker-resolved workspace plus the credential the
// wrapper should use in place of Config.AuthConfig when a matching per-workspace
// record exists in the store.
type workspaceScope struct {
	// Slug is empty when no marker; forwarded to the proxy on SetSession so
	// per-session routing can pick it up.
	Slug       string
	Credential authpkg.Credential
	Origin     authpkg.Origin
	// Matched is true only when a per-workspace record was found; false leaves
	// the wrapper's Config.AuthConfig untouched.
	Matched bool
}

// workspaceCredResolver is the injection seam for tests. Production stays on
// ResolveNoRefreshForWorkspace so the wrapper never blocks a build on a network
// refresh.
type workspaceCredResolver func(ctx context.Context, envs map[string]string, workspaceID string) (authpkg.Credential, authpkg.Origin, bool, error)

//nolint:gochecknoglobals
var defaultWorkspaceCredResolver workspaceCredResolver = func(ctx context.Context, envs map[string]string, workspaceID string) (authpkg.Credential, authpkg.Origin, bool, error) {
	return live.Default(nil).ResolveNoRefreshForWorkspace(ctx, envs, workspaceID)
}

// resolveWorkspaceScope walks up from projectDir looking for the marker and,
// when one is present, resolves the credential for the declared workspace.
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
