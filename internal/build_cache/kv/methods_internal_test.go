//go:build unit

package kv

import (
	"context"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/build/bazel/remote/execution/v2"
)

func decodeRequestMetadata(t *testing.T, serialized string) *remoteexecution.RequestMetadata {
	t.Helper()
	rmd := &remoteexecution.RequestMetadata{}
	require.NoError(t, proto.Unmarshal([]byte(serialized), rmd))

	return rmd
}

func TestGetMethodCallMetadata_PrefersCtxInvocationIDOverClient(t *testing.T) {
	c := &Client{
		clientName:    "test-tool",
		authSource:    staticAuthSource{cfg: authpkg.Credential{Token: "tok", WorkspaceID: "ws"}},
		logger:        log.NewLogger(),
		invocationID:  "wrapper-id",
	}

	ctx := WithInvocationID(context.Background(), "ctx-id")

	md := c.getMethodCallMetadata(ctx, false)
	serialized := md.Get("build.bazel.remote.execution.v2.requestmetadata-bin")
	require.Len(t, serialized, 1)

	rmd := decodeRequestMetadata(t, serialized[0])
	assert.Equal(t, "ctx-id", rmd.ToolInvocationId,
		"ctx-carried InvocationID must win over c.invocationID so concurrent peers never cross-tag")
}

func TestGetMethodCallMetadata_FallsBackToClientInvocationIDWhenCtxEmpty(t *testing.T) {
	c := &Client{
		clientName:    "test-tool",
		authSource:    staticAuthSource{cfg: authpkg.Credential{Token: "tok", WorkspaceID: "ws"}},
		logger:        log.NewLogger(),
		invocationID:  "wrapper-id",
	}

	md := c.getMethodCallMetadata(context.Background(), false)
	serialized := md.Get("build.bazel.remote.execution.v2.requestmetadata-bin")
	require.Len(t, serialized, 1)

	rmd := decodeRequestMetadata(t, serialized[0])
	assert.Equal(t, "wrapper-id", rmd.ToolInvocationId,
		"plain ctx must fall through to the shared c.invocationID (wrapper + ccache flow)")
}
