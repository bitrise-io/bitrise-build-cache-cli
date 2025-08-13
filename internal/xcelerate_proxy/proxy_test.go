package xcelerate_proxy

import (
	"context"
	"errors"
	"net"
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

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate_proxy/mock"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	llvmkv "github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/session"
)

func Test_Proxy_Headers(t *testing.T) {
	var mds []metadata.MD
	capabilitiesClient := &mock.CapabilitiesClientMock{}
	server := NewProxy(
		&mock.KVStorageClientMock{
			GetFunc: func(
				ctx context.Context,
				in *bytestream.ReadRequest,
				opts ...grpc.CallOption,
			) (grpc.ServerStreamingClient[bytestream.ReadResponse], error) {
				md, ok := metadata.FromOutgoingContext(ctx)
				require.True(t, ok)

				mds = append(mds, md)

				return nil, errors.New("not implemented")
			},
		},
		capabilitiesClient,
		"test-token",
		"test-app-slug",
		"test-org-id",
		"test-invocation-id",
		"test-build-slug",
		"test-step-execution-id",
		log.NewLogger(),
	)

	listen := bufconn.Listen(1024 * 1024)
	go func() {
		assert.NoError(t, server.Serve(listen))
	}()

	resolver.SetDefaultScheme("passthrough")
	client, err := grpc.NewClient("bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listen.Dial()
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
	require.Equal(t, mds[0].Get(headerBuildToolMetadataKey), mds[2].Get(headerBuildToolMetadataKey))
	require.Equal(t, mds[0].Get("authorization"), mds[2].Get("authorization"))
	require.Equal(t, mds[0].Get(headerOrgIdMetadataKey), mds[2].Get(headerOrgIdMetadataKey))
	require.NotEqual(t, mds[0].Get(headerRequestMetadataKey), mds[2].Get(headerRequestMetadataKey))
	require.NotEqual(t, mds[0].Get(headerAppIdMetadataKey), mds[2].Get(headerAppIdMetadataKey))
	require.NotEqual(t, mds[0].Get(headerBuildIdMetadataKey), mds[2].Get(headerBuildIdMetadataKey))
	require.NotEqual(t, mds[0].Get(headerStepIdMetadataKey), mds[2].Get(headerStepIdMetadataKey))

	// Check capabilities call count again, it should be called again for the new invocation ID
	_, _ = keyValueDBClient.GetValue(context.Background(), &llvmkv.GetValueRequest{
		Key: []byte("test-key"),
	})
	assert.Len(t, capabilitiesClient.GetCapabilitiesCalls(), 2)
}

func assertMetadata(t *testing.T, md metadata.MD) {
	t.Helper()

	assert.Len(t, md.Get(headerBuildToolMetadataKey), 1)
	assert.Len(t, md.Get("authorization"), 1)
	assert.Len(t, md.Get(headerAppIdMetadataKey), 1)
	assert.Len(t, md.Get(headerOrgIdMetadataKey), 1)
	assert.Len(t, md.Get(headerRequestMetadataKey), 1)
	rmd := &remoteexecution.RequestMetadata{}
	require.NoError(t, proto.Unmarshal([]byte(md.Get(headerRequestMetadataKey)[0]), rmd))
	assert.NotEmpty(t, rmd.GetToolInvocationId())
	assert.NotEmpty(t, rmd.GetToolDetails().GetToolName())
	assert.Len(t, md.Get(headerBuildIdMetadataKey), 1)
	assert.Len(t, md.Get(headerStepIdMetadataKey), 1)
}
