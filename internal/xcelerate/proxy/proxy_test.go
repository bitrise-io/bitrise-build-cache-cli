package proxy_test

import (
	"context"
	"encoding/gob"
	"errors"
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
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy/mocks"
	llvmcas "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/cas"
	llvmkv "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
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
		}, nil)

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

// A cold cache must leave the counter at zero; only a real backend failure raises it.
func Test_Proxy_SessionStatsErrorCounter(t *testing.T) {
	cases := map[string]struct {
		downloadErr error
		wantErrors  int64
		wantMisses  int64
	}{
		"cold cache counts a miss, not an error": {downloadErr: kv.ErrCacheNotFound, wantErrors: 0, wantMisses: 1},
		"backend failure counts an error":        {downloadErr: errors.New("connection refused"), wantErrors: 1, wantMisses: 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kvClient := &mocks.ClientMock{
				DownloadStreamFunc: func(context.Context, io.Writer, string) error { return tc.downloadErr },
			}

			listener := bufconn.Listen(1024 * 1024)
			t.Cleanup(func() { _ = listener.Close() })

			resolver.SetDefaultScheme("passthrough")
			client, err := grpc.NewClient("bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return listener.Dial()
			}), grpc.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)

			go func() {
				p := proxy.NewProxy(kvClient, true, mockLogger, func(string) (log.Logger, error) {
					return mockLogger, nil
				}, nil)
				_ = p.Serve(listener)
			}()

			casClient := llvmcas.NewCASDBServiceClient(client)
			_, err = casClient.Get(context.Background(), &llvmcas.CASGetRequest{
				CasId: &llvmcas.CASDataID{Id: []byte("missing-key")},
			})
			require.NoError(t, err)

			stats, err := session.NewSessionClient(client).GetSessionStats(context.Background(), &emptypb.Empty{})
			require.NoError(t, err)
			assert.Equal(t, tc.wantErrors, stats.GetErrors(), "errors")
			assert.Equal(t, tc.wantMisses, stats.GetMisses(), "misses")
		})
	}
}

func Test_Proxy_SessionStatsKeepsTheFirstErrorMessage(t *testing.T) {
	var calls int
	kvClient := &mocks.ClientMock{
		DownloadStreamFunc: func(context.Context, io.Writer, string) error {
			calls++
			if calls == 1 {
				return errors.New("connection refused")
			}

			return errors.New("a later, different failure")
		},
	}

	listener := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = listener.Close() })

	resolver.SetDefaultScheme("passthrough")
	client, err := grpc.NewClient("bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	go func() {
		p := proxy.NewProxy(kvClient, true, mockLogger, func(string) (log.Logger, error) {
			return mockLogger, nil
		}, nil)
		_ = p.Serve(listener)
	}()

	casClient := llvmcas.NewCASDBServiceClient(client)
	for range 2 {
		_, err = casClient.Get(context.Background(), &llvmcas.CASGetRequest{
			CasId: &llvmcas.CASDataID{Id: []byte("some-key")},
		})
		require.NoError(t, err)
	}

	stats, err := session.NewSessionClient(client).GetSessionStats(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.GetErrors())
	assert.Contains(t, stats.GetFirstError(), "connection refused")
	assert.Contains(t, stats.GetFirstError(), "Get", "the message names the operation")
	assert.NotContains(t, stats.GetFirstError(), "later, different")
}

// The session-stats payload is what the wrapper forwards to analytics.
func Test_Proxy_SessionStatsCarryBlobStats(t *testing.T) {
	const payload = "a blob big enough to clear the throughput floor"

	kvClient := &mocks.ClientMock{
		DownloadStreamFunc: func(_ context.Context, w io.Writer, _ string) error {
			return gob.NewEncoder(w).Encode(struct {
				Data       []byte
				References [][]byte
			}{Data: []byte(payload)})
		},
		UploadStreamToBuildCacheFunc: func(context.Context, io.ReadSeeker, string, int64) error { return nil },
	}

	listener := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = listener.Close() })

	resolver.SetDefaultScheme("passthrough")
	client, err := grpc.NewClient("bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	go func() {
		p := proxy.NewProxy(kvClient, true, mockLogger, func(string) (log.Logger, error) {
			return mockLogger, nil
		}, nil)
		_ = p.Serve(listener)
	}()

	casClient := llvmcas.NewCASDBServiceClient(client)

	getResponse, err := casClient.Get(context.Background(), &llvmcas.CASGetRequest{
		CasId: &llvmcas.CASDataID{Id: []byte("present-key")},
	})
	require.NoError(t, err)
	require.Equal(t, llvmcas.CASGetResponse_SUCCESS, getResponse.GetOutcome())

	putResponse, err := casClient.Put(context.Background(), &llvmcas.CASPutRequest{
		Data: &llvmcas.CASObject{
			Blob: &llvmcas.CASBytes{Contents: &llvmcas.CASBytes_Data{Data: []byte(payload)}},
		},
	})
	require.NoError(t, err)

	// The same key a second time is local dedup, counted apart from the transfers.
	_, err = casClient.Put(context.Background(), &llvmcas.CASPutRequest{
		Data: &llvmcas.CASObject{
			Blob: &llvmcas.CASBytes{Contents: &llvmcas.CASBytes_Data{Data: []byte(payload)}},
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, putResponse.GetCasId().GetId())

	stats, err := session.NewSessionClient(client).GetSessionStats(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	blobStats := blobstats.FromProto(stats.GetCacheBlobStats())
	require.NotNil(t, blobStats)
	assert.Equal(t, blobstats.SchemaVersion, blobStats.SchemaVersion)

	assert.Equal(t, int64(1), blobStats.Download.OpCount)
	assert.Equal(t, stats.GetDownloadedBytes(), blobStats.Download.BytesTotal)
	assert.Equal(t, int64(1), blobStats.Download.LatencyMs.Count)
	assert.Equal(t, int64(1), blobStats.Download.SizeBytes.Count)

	assert.Equal(t, int64(1), blobStats.Upload.OpCount)
	assert.Equal(t, stats.GetUploadedBytes(), blobStats.Upload.BytesTotal)
	assert.Equal(t, int64(1), blobStats.Upload.SkippedAlreadySavedCount)

	// Both blobs are well under 16 KB, so nothing enters the throughput histogram.
	assert.Equal(t, int64(1), blobStats.Download.Throughput.ExcludedSmallOps)
	assert.Zero(t, blobStats.Download.Throughput.Histogram.Count)
	assert.Equal(t, int64(blobstats.ThroughputMinBlobBytes), blobStats.Download.Throughput.MinBlobBytes)
}
