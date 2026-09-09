//go:build integration

package proxy_test

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv/fakebackend"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/build/bazel/remote/execution/v2"
	kvstorage "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/kv_storage"
	llvmcas "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/cas"
	llvmkv "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
)

// Drives the proxy against the fake cache backend over a real kv.Client, so the blob counts
// are asserted over the same code path a build takes rather than against a mocked client.
func Test_Proxy_Integration_BlobStatsOverFakeBackend(t *testing.T) {
	// 32 KB clears the 16 KB throughput floor, so throughput is exercised too.
	payload := make([]byte, 32*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	sessionClient, casClient, kvClient := startProxyAgainstFakeBackend(t)

	saveResponse, err := casClient.Save(context.Background(), &llvmcas.CASSaveRequest{
		Data: &llvmcas.CASBlob{
			Blob: &llvmcas.CASBytes{Contents: &llvmcas.CASBytes_Data{Data: payload}},
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, saveResponse.GetCasId().GetId())

	// The same blob again never reaches the backend.
	_, err = casClient.Save(context.Background(), &llvmcas.CASSaveRequest{
		Data: &llvmcas.CASBlob{
			Blob: &llvmcas.CASBytes{Contents: &llvmcas.CASBytes_Data{Data: payload}},
		},
	})
	require.NoError(t, err)

	loadResponse, err := casClient.Load(context.Background(), &llvmcas.CASLoadRequest{
		CasId: saveResponse.GetCasId(),
	})
	require.NoError(t, err)
	require.Equal(t, llvmcas.CASLoadResponse_SUCCESS, loadResponse.GetOutcome())
	require.Equal(t, payload, loadResponse.GetData().GetBlob().GetData())

	missResponse, err := casClient.Load(context.Background(), &llvmcas.CASLoadRequest{
		CasId: &llvmcas.CASDataID{Id: []byte("never-stored")},
	})
	require.NoError(t, err)
	require.Equal(t, llvmcas.CASLoadResponse_OBJECT_NOT_FOUND, missResponse.GetOutcome())

	_, err = kvClient.PutValue(context.Background(), &llvmkv.PutValueRequest{
		Key:   []byte("kv-key"),
		Value: &llvmkv.Value{Entries: map[string][]byte{"entry": payload}},
	})
	require.NoError(t, err)

	stats, err := sessionClient.GetSessionStats(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	blobStats := blobstats.FromProto(stats.GetCacheBlobStats())
	require.NotNil(t, blobStats)

	// One CAS save plus one KV put; the deduped save is counted apart.
	assert.Equal(t, int64(2), blobStats.Upload.OpCount, "uploads")
	assert.Equal(t, int64(1), blobStats.Upload.SkippedAlreadySavedCount, "deduped saves")
	assert.Equal(t, stats.GetUploadedBytes(), blobStats.Upload.BytesTotal)
	assert.Equal(t, int64(2), blobStats.Upload.LatencyMs.Count)
	assert.Equal(t, int64(2), blobStats.Upload.Throughput.Histogram.Count, "both clear the floor")
	assert.Zero(t, blobStats.Upload.MissCount)

	assert.Equal(t, int64(1), blobStats.Download.OpCount, "downloads")
	assert.Equal(t, int64(1), blobStats.Download.MissCount, "the miss is counted, not timed")
	assert.Equal(t, stats.GetDownloadedBytes(), blobStats.Download.BytesTotal)
	assert.Equal(t, int64(1), blobStats.Download.LatencyMs.Count)
	assert.Equal(t, int64(len(payload)), blobStats.Download.SizeBytes.Sum)
	assert.Positive(t, blobStats.Download.Throughput.P50BytesPerSec)

	// Only transfers are timed, so the latency histogram matches the op count exactly.
	assert.Equal(t, blobStats.Download.OpCount, blobStats.Download.LatencyMs.Count)
	assert.Equal(t, blobStats.Upload.OpCount, blobStats.Upload.LatencyMs.Count)
}

func startProxyAgainstFakeBackend(t *testing.T) (
	session.SessionClient, llvmcas.CASDBServiceClient, llvmkv.KeyValueDBClient,
) {
	t.Helper()

	backend := fakebackend.New(1.0)
	endpoint, stopBackend, err := backend.Serve()
	require.NoError(t, err)
	t.Cleanup(stopBackend)

	resolver.SetDefaultScheme("passthrough")
	backendConn, err := grpc.NewClient(
		strings.TrimPrefix(endpoint, "grpc://"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = backendConn.Close() })

	//nolint:exhaustruct // the injected clients replace the dialed pool
	cacheClient, err := kv.NewClient(kv.NewClientParams{
		ClientName:         "blob-stats-integration",
		Logger:             log.NewLogger(),
		BitriseKVClient:    kvstorage.NewKVStorageClient(backendConn),
		CapabilitiesClient: remoteexecution.NewCapabilitiesClient(backendConn),
	})
	require.NoError(t, err)

	listener := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = listener.Close() })

	logger := log.NewLogger()
	go func() {
		p := proxy.NewProxy(cacheClient, true, logger, func(string) (log.Logger, error) {
			return logger, nil
		}, nil)
		_ = p.Serve(listener)
	}()

	proxyConn, err := grpc.NewClient("bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = proxyConn.Close() })

	return session.NewSessionClient(proxyConn),
		llvmcas.NewCASDBServiceClient(proxyConn),
		llvmkv.NewKeyValueDBClient(proxyConn)
}
