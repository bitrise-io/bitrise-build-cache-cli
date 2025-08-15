package cmd

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/mock"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	llvmkv "github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/session"
)

func Test_XcelerateProxy(t *testing.T) {
	var mds []metadata.MD
	capabilitiesClient := &mock.CapabilitiesClientMock{
		GetCapabilitiesFunc: func(ctx context.Context, in *remoteexecution.GetCapabilitiesRequest, opts ...grpc.CallOption) (*remoteexecution.ServerCapabilities, error) {
			return &remoteexecution.ServerCapabilities{}, nil
		},
	}

	envVars := map[string]string{
		"BITRISE_IO":                       "true",
		"REMOTE_CACHE_TOKEN":               "test-token",
		"BITRISE_BUILD_CACHE_ENDPOINT":     "grpc://bufnet",
		"BITRISE_APP_SLUG":                 "test-app-slug",
		"BITRISE_BUILD_CACHE_WORKSPACE_ID": "test-org-id",
		"INVOCATION_ID":                    "test-invocation-id",
		"BITRISE_BUILD_SLUG":               "test-build-slug",
		"BITRISE_STEP_EXECUTION_ID":        "test-step-execution-id",
	}

	listener := bufconn.Listen(1024 * 1024)
	defer listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = startXcodeCacheProxy(
			ctx,
			log.NewLogger(),
			func(key string) string {
				if value, exists := envVars[key]; exists {
					return value
				}

				t.Logf("Environment variable %s not set, returning empty string", key)

				return ""
			},
			func(name string, v ...string) (string, error) {
				output, err := exec.Command(name, v...).Output() //nolint:noctx

				return string(output), err
			},
			&mock.KVStorageClientMock{
				GetFunc: func(
					ctx context.Context,
					in *bytestream.ReadRequest,
					opts ...grpc.CallOption,
				) (grpc.ServerStreamingClient[bytestream.ReadResponse], error) {
					md, ok := metadata.FromOutgoingContext(ctx)
					assert.True(t, ok)

					mds = append(mds, md)

					return nil, errors.New("not implemented")
				},
			},
			capabilitiesClient,
			listener,
		)
	}()

	resolver.SetDefaultScheme("passthrough")
	client, err := grpc.NewClient("bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	keyValueDBClient := llvmkv.NewKeyValueDBClient(client)

	// First call
	value, err := keyValueDBClient.GetValue(context.Background(), &llvmkv.GetValueRequest{
		Key: []byte("test-key"),
	})
	require.NoError(t, err)
	require.NotNil(t, value)
	// we should get an error because the mock is returning one, but it's all good, we only interested in the headers
	require.NotNil(t, value.GetError())

	// Check the metadata
	require.Len(t, mds, 1)
	assertMetadata(t, mds[0])

	// Check capabilities call count, it should be called once per invocation ID
	_, _ = keyValueDBClient.GetValue(context.Background(), &llvmkv.GetValueRequest{
		Key: []byte("test-key"),
	})
	assert.Len(t, capabilitiesClient.GetCapabilitiesCalls(), 1)

	// it's time to change the session
	_, err = session.NewSessionClient(client).SetSession(context.Background(), &session.SetSessionRequest{
		InvocationId: "new-invocation-id",
		AppSlug:      "new-app-slug",
		BuildSlug:    "new-build-slug",
		StepSlug:     "new-step-execution-id",
	})
	require.NoError(t, err)

	// Second call
	_, err = keyValueDBClient.GetValue(context.Background(), &llvmkv.GetValueRequest{
		Key: []byte("test-key"),
	})
	require.NoError(t, err)

	require.Len(t, mds, 3)
	assertMetadata(t, mds[2])

	// make sure only the fields that are expected to change are different
	require.Equal(t, mds[0].Get("x-flare-buildtool"), mds[2].Get("x-flare-buildtool"))
	require.Equal(t, mds[0].Get("authorization"), mds[2].Get("authorization"))
	require.Equal(t, mds[0].Get("x-org-id"), mds[2].Get("x-org-id"))
	require.NotEqual(t, mds[0].Get("build.bazel.remote.execution.v2.requestmetadata-bin"), mds[2].Get("build.bazel.remote.execution.v2.requestmetadata-bin"))
	require.NotEqual(t, mds[0].Get("x-app-id"), mds[2].Get("x-app-id"))
	require.NotEqual(t, mds[0].Get("x-flare-build-id"), mds[2].Get("x-flare-build-id"))
	require.NotEqual(t, mds[0].Get("x-flare-step-id"), mds[2].Get("x-flare-step-id"))

	// Check capabilities call count again, it should be called again for the new invocation ID
	_, _ = keyValueDBClient.GetValue(context.Background(), &llvmkv.GetValueRequest{
		Key: []byte("test-key"),
	})
	assert.Len(t, capabilitiesClient.GetCapabilitiesCalls(), 2)
}

func assertMetadata(t *testing.T, md metadata.MD) {
	t.Helper()

	assert.Len(t, md.Get("x-flare-buildtool"), 1)
	assert.Len(t, md.Get("authorization"), 1)
	assert.Len(t, md.Get("x-app-id"), 1)
	assert.Len(t, md.Get("x-org-id"), 1)
	assert.Len(t, md.Get("build.bazel.remote.execution.v2.requestmetadata-bin"), 1)
	rmd := &remoteexecution.RequestMetadata{}
	require.NoError(t, proto.Unmarshal([]byte(md.Get("build.bazel.remote.execution.v2.requestmetadata-bin")[0]), rmd))
	assert.NotEmpty(t, rmd.GetToolInvocationId())
	assert.NotEmpty(t, rmd.GetToolDetails().GetToolName())
	assert.Len(t, md.Get("x-flare-build-id"), 1)
	assert.Len(t, md.Get("x-flare-step-id"), 1)
}
