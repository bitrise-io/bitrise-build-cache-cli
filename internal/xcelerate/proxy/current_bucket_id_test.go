//go:build unit

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

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy/mocks"
	llvmkv "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/kv"
)

func TestProxy_CurrentBucketID_RoundTrip(t *testing.T) {
	p := proxy.NewProxy(&mocks.ClientMock{}, false, mockLogger, func(string) (log.Logger, error) {
		return mockLogger, nil
	}, nil)

	assert.Empty(t, p.CurrentBucketID())

	p.SetCurrentBucketID("bucket-xyz")
	assert.Equal(t, "bucket-xyz", p.CurrentBucketID())

	p.SetCurrentBucketID("")
	assert.Empty(t, p.CurrentBucketID())
}

func TestProxy_InterceptorStampsBucketIDOnRPCCtx(t *testing.T) {
	gotID := make(chan string, 1)
	kvClient := &mocks.ClientMock{
		DownloadStreamFunc: func(ctx context.Context, _ io.Writer, _ string) error {
			id, _ := kv.InvocationIDFromContext(ctx)
			gotID <- id

			return kv.ErrCacheNotFound
		},
		GetCapabilitiesWithRetryFunc: func(_ context.Context) error { return nil },
	}

	listener := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = listener.Close() })

	resolver.SetDefaultScheme("passthrough")
	cc, err := grpc.NewClient("bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cc.Close() })

	p := proxy.NewProxy(kvClient, false, mockLogger, func(string) (log.Logger, error) {
		return mockLogger, nil
	}, nil)
	go func() { _ = p.Serve(listener) }()

	p.SetCurrentBucketID("bucket-under-test")

	kvCli := llvmkv.NewKeyValueDBClient(cc)
	_, _ = kvCli.GetValue(context.Background(), &llvmkv.GetValueRequest{Key: []byte("k")})

	assert.Equal(t, "bucket-under-test", <-gotID)
}
