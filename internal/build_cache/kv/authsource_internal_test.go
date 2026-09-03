//go:build unit

package kv

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

type dynamicAuthSource struct {
	calls atomic.Int64
	cfgs  []authpkg.Credential
}

func (d *dynamicAuthSource) Get(context.Context) authpkg.Credential {
	i := d.calls.Add(1) - 1
	if int(i) >= len(d.cfgs) {
		return d.cfgs[len(d.cfgs)-1]
	}

	return d.cfgs[i]
}

// getMethodCallMetadata re-reads the AuthSource on every call — proves per-RPC
// token freshness when the token rotates across the TTL boundary.
func TestClient_getMethodCallMetadata_RefreshesPerCall(t *testing.T) {
	src := &dynamicAuthSource{
		cfgs: []authpkg.Credential{
			{Token: "tok-1", WorkspaceID: "ws-1"},
			{Token: "tok-2", WorkspaceID: "ws-2"},
		},
	}

	c := &Client{
		clientName: "test-tool",
		authSource: src,
		logger:     log.NewLogger(),
	}

	md1 := c.getMethodCallMetadata(t.Context(), false)
	md2 := c.getMethodCallMetadata(t.Context(), false)

	require.Equal(t, []string{"bearer tok-1"}, md1.Get("authorization"))
	require.Equal(t, []string{"bearer tok-2"}, md2.Get("authorization"))
	assert.Equal(t, []string{"ws-1"}, md1.Get("x-org-id"))
	assert.Equal(t, []string{"ws-2"}, md2.Get("x-org-id"))
}

// A stable AuthSource behaves like the old fixed AuthConfig — successive calls
// return identical auth headers. Guards against accidental non-determinism.
func TestClient_getMethodCallMetadata_StableWhenSourceStable(t *testing.T) {
	src := staticAuthSource{cfg: authpkg.Credential{Token: "tok", WorkspaceID: "ws"}}

	c := &Client{
		clientName: "test-tool",
		authSource: src,
		logger:     log.NewLogger(),
	}

	md1 := c.getMethodCallMetadata(t.Context(), false)
	md2 := c.getMethodCallMetadata(t.Context(), false)

	assert.Equal(t, md1.Get("authorization"), md2.Get("authorization"))
	assert.Equal(t, md1.Get("x-org-id"), md2.Get("x-org-id"))
}

type workspaceAuthSourceFake struct {
	defaultCred   authpkg.Credential
	perWorkspace  map[string]authpkg.Credential
	lastWorkspace atomic.Value // holds string
}

func (w *workspaceAuthSourceFake) Get(context.Context) authpkg.Credential {
	return w.defaultCred
}

func (w *workspaceAuthSourceFake) GetForWorkspace(_ context.Context, workspaceID string) authpkg.Credential {
	w.lastWorkspace.Store(workspaceID)
	if cred, ok := w.perWorkspace[workspaceID]; ok {
		return cred
	}

	return w.defaultCred
}

func TestClient_getMethodCallMetadata_PerWorkspaceSwap(t *testing.T) {
	src := &workspaceAuthSourceFake{
		defaultCred: authpkg.Credential{Token: "default-tok", WorkspaceID: "default-ws"},
		perWorkspace: map[string]authpkg.Credential{
			"acme": {Token: "acme-tok", WorkspaceID: "acme"},
		},
	}

	c := &Client{
		clientName: "test-tool",
		authSource: src,
		logger:     log.NewLogger(),
	}

	// No session workspace → machine-wide credential.
	md1 := c.getMethodCallMetadata(t.Context(), false)
	assert.Equal(t, []string{"bearer default-tok"}, md1.Get("authorization"))
	assert.Equal(t, []string{"default-ws"}, md1.Get("x-org-id"))

	// Session workspace → per-workspace credential resolved.
	c.ChangeSession("inv-1", "app", "build", "step", "acme")
	md2 := c.getMethodCallMetadata(t.Context(), false)
	assert.Equal(t, []string{"bearer acme-tok"}, md2.Get("authorization"))
	assert.Equal(t, []string{"acme"}, md2.Get("x-org-id"))

	// Clearing back to empty falls back to machine-wide.
	c.ChangeSession("inv-2", "app", "build", "step", "")
	md3 := c.getMethodCallMetadata(t.Context(), false)
	assert.Equal(t, []string{"bearer default-tok"}, md3.Get("authorization"))
}

// A plain AuthSource that does not implement WorkspaceAuthSource never sees the
// session workspace ID: the swap is skipped and the machine-wide credential is
// returned. Guards against source implementations changing behavior silently.
func TestClient_getMethodCallMetadata_PlainSourceIgnoresWorkspace(t *testing.T) {
	src := staticAuthSource{cfg: authpkg.Credential{Token: "static-tok", WorkspaceID: "static-ws"}}

	c := &Client{
		clientName: "test-tool",
		authSource: src,
		logger:     log.NewLogger(),
	}

	c.ChangeSession("inv", "app", "build", "step", "acme")

	md := c.getMethodCallMetadata(t.Context(), false)
	assert.Equal(t, []string{"bearer static-tok"}, md.Get("authorization"))
	assert.Equal(t, []string{"static-ws"}, md.Get("x-org-id"))
}
