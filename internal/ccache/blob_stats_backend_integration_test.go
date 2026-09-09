//go:build integration

package ccache

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv/fakebackend"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache/protocol"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/build/bazel/remote/execution/v2"
	kvstorage "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/kv_storage"
)

// Drives the storage helper against the fake cache backend over a real kv.Client, so the blob
// counts are asserted over the same code path a build takes rather than against a mocked client.
func Test_IpcServer_Integration_BlobStatsOverFakeBackend(t *testing.T) {
	// 32 KB clears the 16 KB throughput floor, so throughput is exercised too.
	value := make([]byte, 32*1024)
	for i := range value {
		value[i] = byte(i % 251)
	}

	socketPath := integrationTempSocket(t, "bs-be.sock")
	cacheClient := realClientAgainstFakeBackend(t)

	srv, cancel, serverDone := startTestServer(t, socketPath, cacheClient, nil)
	defer cancel()

	key := []byte{0xAB, 0xCD}
	require.Equal(t, byte(protocol.ResponseOK), sendRequest(t, socketPath, buildIntegrationPutRequest(key, value)))

	resp, data := sendGetAndReadValue(t, socketPath, key)
	require.Equal(t, byte(protocol.ResponseOK), resp)
	require.Equal(t, value, data)

	missResp, _ := sendGetAndReadValue(t, socketPath, []byte{0x01, 0x02})
	require.Equal(t, byte(protocol.ResponseNoop), missResp)

	snapshot, err := SendGetBlobStats(context.Background(), socketPath)
	require.NoError(t, err)
	require.NotNil(t, snapshot)

	assert.Equal(t, int64(1), snapshot.Upload.OpCount)
	assert.Equal(t, int64(len(value)), snapshot.Upload.BytesTotal)
	assert.Equal(t, int64(1), snapshot.Upload.Throughput.Histogram.Count, "32 KB clears the floor")

	assert.Equal(t, int64(1), snapshot.Download.OpCount)
	assert.Equal(t, int64(len(value)), snapshot.Download.BytesTotal)
	assert.Equal(t, int64(1), snapshot.Download.MissCount, "the miss is counted, not timed")
	assert.Equal(t, int64(1), snapshot.Download.LatencyMs.Count)
	assert.Positive(t, snapshot.Download.Throughput.P50BytesPerSec)

	assert.Equal(t, blobstats.SchemaVersion, snapshot.SchemaVersion)

	// The session summary is derived from the snapshot, so the two cannot drift.
	dl, ul := srv.SessionBytes()
	assert.Equal(t, snapshot.Download.BytesTotal, dl)
	assert.Equal(t, snapshot.Upload.BytesTotal, ul)

	effectiveness := srv.SessionEffectiveness()
	assert.Equal(t, snapshot.Download.OpCount, effectiveness.Hits)
	assert.Equal(t, snapshot.Download.OpCount+snapshot.Download.MissCount, effectiveness.Total)
	assert.Equal(t, snapshot.Download.BytesTotal, effectiveness.DownloadBytes)
	assert.Equal(t, snapshot.Upload.BytesTotal, effectiveness.UploadBytes)

	cancel()
	<-serverDone
}

func realClientAgainstFakeBackend(t *testing.T) Client {
	t.Helper()

	endpoint, stopBackend, err := fakebackend.New(1.0).Serve()
	require.NoError(t, err)
	t.Cleanup(stopBackend)

	conn, err := grpc.NewClient(
		strings.TrimPrefix(endpoint, "grpc://"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	//nolint:exhaustruct // the injected clients replace the dialed pool
	client, err := kv.NewClient(kv.NewClientParams{
		ClientName:         "blob-stats-integration",
		Logger:             noOpLogger(),
		BitriseKVClient:    kvstorage.NewKVStorageClient(conn),
		CapabilitiesClient: remoteexecution.NewCapabilitiesClient(conn),
	})
	require.NoError(t, err)

	return client
}
