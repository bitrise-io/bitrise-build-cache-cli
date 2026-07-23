//go:build unit

package kv

import (
	"sync/atomic"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

type dynamicAuthSource struct {
	calls atomic.Int64
	cfgs  []common.CacheAuthConfig
}

func (d *dynamicAuthSource) Get() common.CacheAuthConfig {
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
		cfgs: []common.CacheAuthConfig{
			{AuthToken: "tok-1", WorkspaceID: "ws-1"},
			{AuthToken: "tok-2", WorkspaceID: "ws-2"},
		},
	}

	c := &Client{
		clientName: "test-tool",
		authSource: src,
		logger:     log.NewLogger(),
	}

	md1 := c.getMethodCallMetadata(false)
	md2 := c.getMethodCallMetadata(false)

	require.Equal(t, []string{"bearer tok-1"}, md1.Get("authorization"))
	require.Equal(t, []string{"bearer tok-2"}, md2.Get("authorization"))
	assert.Equal(t, []string{"ws-1"}, md1.Get("x-org-id"))
	assert.Equal(t, []string{"ws-2"}, md2.Get("x-org-id"))
}

// A stable AuthSource behaves like the old fixed AuthConfig — successive calls
// return identical auth headers. Guards against accidental non-determinism.
func TestClient_getMethodCallMetadata_StableWhenSourceStable(t *testing.T) {
	src := staticAuthSource{cfg: common.CacheAuthConfig{AuthToken: "tok", WorkspaceID: "ws"}}

	c := &Client{
		clientName: "test-tool",
		authSource: src,
		logger:     log.NewLogger(),
	}

	md1 := c.getMethodCallMetadata(false)
	md2 := c.getMethodCallMetadata(false)

	assert.Equal(t, md1.Get("authorization"), md2.Get("authorization"))
	assert.Equal(t, md1.Get("x-org-id"), md2.Get("x-org-id"))
}
