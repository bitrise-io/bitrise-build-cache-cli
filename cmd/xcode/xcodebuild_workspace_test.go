//go:build unit

package xcode

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func writeMarker(t *testing.T, dir, workspace string) {
	t.Helper()
	require.NoError(t, utils.DefaultOsProxy{}.WriteFile(
		filepath.Join(dir, ".bitrise-build-cache.json"),
		[]byte(`{"workspace":"`+workspace+`"}`),
		0o600,
	))
}

func TestResolveWorkspaceScope_MarkerAtProjectDir_SwapsCredential(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "acme")

	resolver := func(_ context.Context, _ map[string]string, slug string) (authpkg.Credential, authpkg.Origin, bool, error) {
		return authpkg.Credential{Token: "acme-tok", WorkspaceID: slug}, authpkg.Origin{Backend: authpkg.BackendKeychain}, true, nil
	}

	scope := resolveWorkspaceScopeWith(context.Background(), dir, map[string]string{}, utils.DefaultOsProxy{}, io.Discard, resolver)

	assert.Equal(t, "acme", scope.Slug)
	assert.True(t, scope.Matched)
	assert.Equal(t, "acme-tok", scope.Credential.Token)
}

func TestResolveWorkspaceScope_MarkerInParent_WalksUp(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, "parent-ws")
	sub := filepath.Join(root, "packages", "app")
	require.NoError(t, utils.DefaultOsProxy{}.MkdirAll(sub, 0o700))

	resolver := func(_ context.Context, _ map[string]string, slug string) (authpkg.Credential, authpkg.Origin, bool, error) {
		return authpkg.Credential{Token: "ws-tok", WorkspaceID: slug}, authpkg.Origin{Backend: authpkg.BackendKeychain}, true, nil
	}

	scope := resolveWorkspaceScopeWith(context.Background(), sub, map[string]string{}, utils.DefaultOsProxy{}, io.Discard, resolver)

	assert.Equal(t, "parent-ws", scope.Slug)
	assert.True(t, scope.Matched)
}

func TestResolveWorkspaceScope_NoMarker_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()

	resolver := func(_ context.Context, _ map[string]string, _ string) (authpkg.Credential, authpkg.Origin, bool, error) {
		t.Fatalf("resolver must not be called when no marker exists")

		return authpkg.Credential{}, authpkg.Origin{}, false, nil
	}

	scope := resolveWorkspaceScopeWith(context.Background(), dir, map[string]string{}, utils.DefaultOsProxy{}, io.Discard, resolver)

	assert.Empty(t, scope.Slug)
	assert.False(t, scope.Matched)
}

func TestResolveWorkspaceScope_MalformedMarker_WarnsAndFallsBack(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, utils.DefaultOsProxy{}.WriteFile(
		filepath.Join(dir, ".bitrise-build-cache.json"),
		[]byte(`{not json`),
		0o600,
	))

	warn := &bytes.Buffer{}
	resolver := func(_ context.Context, _ map[string]string, _ string) (authpkg.Credential, authpkg.Origin, bool, error) {
		t.Fatalf("resolver must not be called when the marker is unreadable")

		return authpkg.Credential{}, authpkg.Origin{}, false, nil
	}

	scope := resolveWorkspaceScopeWith(context.Background(), dir, map[string]string{}, utils.DefaultOsProxy{}, warn, resolver)

	assert.Empty(t, scope.Slug)
	assert.False(t, scope.Matched)
	assert.Contains(t, warn.String(), "project marker")
}

func TestResolveWorkspaceScope_MarkerNamesUnknownWorkspace_SlugSetMatchedFalse(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "declared")

	resolver := func(_ context.Context, _ map[string]string, _ string) (authpkg.Credential, authpkg.Origin, bool, error) {
		// Machine-wide fallback — the resolver reports matched=false so the
		// wrapper keeps its own AuthConfig regardless of what WorkspaceID the
		// returned credential carries.
		return authpkg.Credential{Token: "machine-tok", WorkspaceID: "machine-ws"}, authpkg.Origin{Backend: authpkg.BackendKeychain}, false, nil
	}

	scope := resolveWorkspaceScopeWith(context.Background(), dir, map[string]string{}, utils.DefaultOsProxy{}, io.Discard, resolver)

	assert.Equal(t, "declared", scope.Slug, "slug must still be forwarded so the proxy sees which workspace was requested")
	assert.False(t, scope.Matched, "no matched credential means the wrapper keeps its own AuthConfig")
	assert.Empty(t, scope.Credential.Token)
}

func TestResolveWorkspaceScope_ResolverError_KeepsSlug_DropsCredential(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "acme")

	resolver := func(_ context.Context, _ map[string]string, _ string) (authpkg.Credential, authpkg.Origin, bool, error) {
		return authpkg.Credential{}, authpkg.Origin{}, false, errors.New("store unavailable")
	}

	scope := resolveWorkspaceScopeWith(context.Background(), dir, map[string]string{}, utils.DefaultOsProxy{}, io.Discard, resolver)

	assert.Equal(t, "acme", scope.Slug)
	assert.False(t, scope.Matched)
}
