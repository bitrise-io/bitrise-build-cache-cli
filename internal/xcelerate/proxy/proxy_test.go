package proxy_test

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/test/bufconn"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/proxy"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/proxy/mocks"
	llvmcas "github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/cas"
	llvmkv "github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/kv"
)

func Test_Proxy_PushDisabled(t *testing.T) {
	kvClient := &mocks.ClientMock{
		DownloadStreamFunc: func(ctx context.Context, writer io.Writer, key string) error {
			return kv.ErrCacheNotFound
		},
	}

	listener := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
	})

	resolver.SetDefaultScheme("passthrough")
	client, err := grpc.NewClient("bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	go func() {
		p := proxy.NewProxy(kvClient, false, mockLogger, func(invocationID string) (log.Logger, error) {
			return mockLogger, nil
		})

		_ = p.Serve(listener)
	}()

	// test GetValue / PutValue
	keyValueDBClient := llvmkv.NewKeyValueDBClient(client)

	_, err = keyValueDBClient.PutValue(context.Background(), &llvmkv.PutValueRequest{
		Key: []byte("test"),
		Value: &llvmkv.Value{
			Entries: map[string][]byte{
				"test": []byte("data"),
			},
		},
	})
	require.NoError(t, err)
	require.Empty(t, kvClient.UploadStreamToBuildCacheCalls()) // ensure no upload was attempted

	getValueResponse, err := keyValueDBClient.GetValue(context.Background(), &llvmkv.GetValueRequest{
		Key: []byte("test"),
	})
	require.NoError(t, err)
	assert.Equal(t, llvmkv.GetValueResponse_KEY_NOT_FOUND, getValueResponse.GetOutcome())
	assert.Nil(t, getValueResponse.GetError())

	// test Load / Save
	casdbServiceClient := llvmcas.NewCASDBServiceClient(client)
	saveResponse, err := casdbServiceClient.Save(context.Background(), &llvmcas.CASSaveRequest{
		Data: &llvmcas.CASBlob{
			Blob: &llvmcas.CASBytes{
				Contents: &llvmcas.CASBytes_Data{
					Data: []byte("data"),
				},
			},
		},
	})
	require.NoError(t, err)

	require.Empty(t, kvClient.UploadStreamToBuildCacheCalls()) // ensure no upload was attempted

	assert.NotEmpty(t, saveResponse.GetCasId().GetId())

	loadResponse, err := casdbServiceClient.Load(context.Background(), &llvmcas.CASLoadRequest{
		CasId: saveResponse.GetCasId(),
	})
	require.NoError(t, err)
	assert.Equal(t, llvmcas.CASLoadResponse_OBJECT_NOT_FOUND, loadResponse.GetOutcome())
	assert.Nil(t, loadResponse.GetError())

	// test Get / Put
	putResponse, err := casdbServiceClient.Put(context.Background(), &llvmcas.CASPutRequest{
		Data: &llvmcas.CASObject{
			Blob: &llvmcas.CASBytes{
				Contents: &llvmcas.CASBytes_Data{
					Data: []byte("data"),
				},
			},
		},
	})
	require.NoError(t, err)
	require.Empty(t, kvClient.UploadStreamToBuildCacheCalls()) // ensure no upload was attempted

	assert.NotEmpty(t, putResponse.GetCasId().GetId())

	getResponse, err := casdbServiceClient.Get(context.Background(), &llvmcas.CASGetRequest{
		CasId: putResponse.GetCasId(),
	})
	require.NoError(t, err)
	assert.Equal(t, llvmcas.CASGetResponse_OBJECT_NOT_FOUND, getResponse.GetOutcome())
	assert.Nil(t, getResponse.GetError())
}
