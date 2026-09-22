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

// A token that still carries a trailing newline reaches getMethodCallMetadata
// and produces an "authorization" header value with a non-printable byte —
// which grpc/metadata rejects client-side before the RPC leaves the process.
// TokenSet.Credential() trims upstream so this never happens; the assertion
// documents the byte-level failure so a future path that re-introduces
// newlines downstream can't silently regress.
func TestClient_getMethodCallMetadata_TrailingNewlineTokenLeavesInvalidHeader(t *testing.T) {
	src := staticAuthSource{cfg: authpkg.Credential{Token: "tok\n", WorkspaceID: "ws"}}

	c := &Client{
		clientName: "test-tool",
		authSource: src,
		logger:     log.NewLogger(),
	}

	md := c.getMethodCallMetadata(t.Context(), false)
	authHeader := md.Get("authorization")
	require.Len(t, authHeader, 1)

	hasNonPrintable := false
	for i := 0; i < len(authHeader[0]); i++ {
		if b := authHeader[0][i]; b < 0x20 || b > 0x7E {
			hasNonPrintable = true

			break
		}
	}
	assert.True(t, hasNonPrintable,
		"a token carrying a control byte at this layer would build a header value gRPC rejects with %q — sanitise before reaching the client",
		"non-printable ASCII characters")
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
