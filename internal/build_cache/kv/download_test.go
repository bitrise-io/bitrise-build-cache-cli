package kv_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv/mocks"
)

var downloadTestData = []byte("test content") //nolint:gochecknoglobals

func TestClient_DownloadStream_AllGood(t *testing.T) {
	streamingClientMock := mocks.NewServerStreamingClientMock[bytestream.ReadResponse]([]mocks.RecvResult[bytestream.ReadResponse]{
		{
			Response: &bytestream.ReadResponse{Data: downloadTestData},
			Metadata: map[string]string{
				"x-flare-blob-validation-sha256": "6ae8a75555209fd6c44157c0aed8016e763ff435a19cf186f76863140143ff72",
			},
		},
		{
			Error: io.EOF,
		},
	})

	client, err := kv.NewClient(kv.NewClientParams{
		Logger: mockLogger,
		BitriseKVClient: &mocks.KVStorageClientMock{
			GetFunc: func(ctx context.Context, in *bytestream.ReadRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[bytestream.ReadResponse], error) {
				return streamingClientMock, nil
			},
		},
		DownloadRetryWait: 1, // to make tests faster
	})
	require.NoError(t, err)

	destination := bytes.NewBuffer(nil)

	err = client.DownloadStream(context.Background(), destination, "test-key")
	require.NoError(t, err)

	assert.Equal(t, downloadTestData, destination.Bytes())
}

func TestClient_DownloadStream_FirstAttemptFails(t *testing.T) {
	const offset int64 = 4

	streamingClientMock := mocks.NewServerStreamingClientMock[bytestream.ReadResponse]([]mocks.RecvResult[bytestream.ReadResponse]{
		{
			Response: &bytestream.ReadResponse{Data: downloadTestData[:offset]},
		},
		{
			Error: errors.New("some error"),
		},
		{
			Response: &bytestream.ReadResponse{Data: downloadTestData[offset:]},
		},
		{
			Error: io.EOF,
		},
	})
	kvClient := &mocks.KVStorageClientMock{
		GetFunc: func(ctx context.Context, in *bytestream.ReadRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[bytestream.ReadResponse], error) {
			return streamingClientMock, nil
		},
	}
	client, err := kv.NewClient(kv.NewClientParams{
		Logger:            mockLogger,
		BitriseKVClient:   kvClient,
		DownloadRetryWait: 1, // to make tests faster
	})
	require.NoError(t, err)

	destination := bytes.NewBuffer(nil)

	err = client.DownloadStream(context.Background(), destination, "test-key")
	require.NoError(t, err)

	require.Len(t, kvClient.GetCalls(), 2)
	assert.Equal(t, offset, kvClient.GetCalls()[1].In.GetReadOffset())

	assert.Equal(t, string(downloadTestData), destination.String())
}

func TestClient_DownloadStream_AllAttemptsFail(t *testing.T) {
	streamingClientMock := mocks.NewServerStreamingClientMock[bytestream.ReadResponse]([]mocks.RecvResult[bytestream.ReadResponse]{
		{
			Error: errors.New("some error"),
		},
		{
			Error: errors.New("some error"),
		},
		{
			Error: errors.New("some error"),
		},
	})
	kvClient := &mocks.KVStorageClientMock{
		GetFunc: func(ctx context.Context, in *bytestream.ReadRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[bytestream.ReadResponse], error) {
			return streamingClientMock, nil
		},
	}
	downloadRetry := uint(2)
	client, err := kv.NewClient(kv.NewClientParams{
		Logger:            mockLogger,
		BitriseKVClient:   kvClient,
		DownloadRetry:     downloadRetry,
		DownloadRetryWait: 1, // to make tests faster
	})
	require.NoError(t, err)

	destination := bytes.NewBuffer(nil)

	err = client.DownloadStream(context.Background(), destination, "test-key")
	require.Error(t, err)

	require.Len(t, kvClient.GetCalls(), int(downloadRetry)+1)
}

func TestClient_DownloadStream_NonRetryableError(t *testing.T) {
	streamingClientMock := mocks.NewServerStreamingClientMock[bytestream.ReadResponse]([]mocks.RecvResult[bytestream.ReadResponse]{
		{
			Error: status.New(codes.Unauthenticated, "some error").Err(),
		},
	})
	kvClient := &mocks.KVStorageClientMock{
		GetFunc: func(ctx context.Context, in *bytestream.ReadRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[bytestream.ReadResponse], error) {
			return streamingClientMock, nil
		},
	}
	client, err := kv.NewClient(kv.NewClientParams{
		Logger:            mockLogger,
		BitriseKVClient:   kvClient,
		DownloadRetryWait: 1, // to make tests faster
	})
	require.NoError(t, err)

	destination := bytes.NewBuffer(nil)

	err = client.DownloadStream(context.Background(), destination, "test-key")
	require.Error(t, err)

	require.Len(t, kvClient.GetCalls(), 1)
}

func TestClient_DownloadStream_MismatchValidation(t *testing.T) {
	streamingClientMock := mocks.NewServerStreamingClientMock[bytestream.ReadResponse]([]mocks.RecvResult[bytestream.ReadResponse]{
		{
			Response: &bytestream.ReadResponse{Data: downloadTestData},
			Metadata: map[string]string{
				"x-flare-blob-validation-sha256": "3fc6540b6002f7622d978ea8c6fcb6a661089de0f4952f42390a694107269893",
			},
		},
		{
			Error: io.EOF,
		},
	})

	client, err := kv.NewClient(kv.NewClientParams{
		Logger: mockLogger,
		BitriseKVClient: &mocks.KVStorageClientMock{
			GetFunc: func(ctx context.Context, in *bytestream.ReadRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[bytestream.ReadResponse], error) {
				return streamingClientMock, nil
			},
		},
		DownloadRetryWait: 1, // to make tests faster
	})
	require.NoError(t, err)

	destination := bytes.NewBuffer(nil)

	err = client.DownloadStream(context.Background(), destination, "test-key")
	require.Error(t, err)
}
