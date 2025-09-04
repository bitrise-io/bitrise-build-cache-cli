package kv_test

import (
	"bytes"
	"context"
	"errors"
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

var uploadTestData = []byte("test content") //nolint:gochecknoglobals

func TestClient_UploadStreamToBuildCache_AllGood(t *testing.T) {
	client, err := kv.NewClient(kv.NewClientParams{
		Logger: mockLogger,
		BitriseKVClient: &mocks.KVStorageClientMock{
			PutFunc: func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[bytestream.WriteRequest, bytestream.WriteResponse], error) {
				return mocks.NewClientStreamClientMock[bytestream.WriteRequest, bytestream.WriteResponse](
					&bytestream.WriteResponse{
						CommittedSize: int64(len(uploadTestData)),
					},
					[]error{nil},
				), nil
			},
		},
		UploadRetryWait: 1, // to make tests faster
	})
	require.NoError(t, err)

	buffer := bytes.NewBuffer(uploadTestData)

	err = client.UploadStreamToBuildCache(context.Background(), buffer, "test-key", int64(buffer.Len()))
	require.NoError(t, err)
}

func TestClient_UploadStreamToBuildCache_FirstAttemptFails(t *testing.T) {
	streamClientMock := mocks.NewClientStreamClientMock[bytestream.WriteRequest, bytestream.WriteResponse](
		&bytestream.WriteResponse{
			CommittedSize: int64(len(uploadTestData)),
		},
		[]error{errors.New("some error"), nil},
	)

	client, err := kv.NewClient(kv.NewClientParams{
		Logger: mockLogger,
		BitriseKVClient: &mocks.KVStorageClientMock{
			PutFunc: func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[bytestream.WriteRequest, bytestream.WriteResponse], error) {
				return streamClientMock, nil
			},
		},
		UploadRetryWait: 1, // to make tests faster
	})
	require.NoError(t, err)

	buffer := bytes.NewBuffer(uploadTestData)

	err = client.UploadStreamToBuildCache(context.Background(), buffer, "test-key", int64(buffer.Len()))
	require.NoError(t, err) // should succeed after retry

	require.Len(t, streamClientMock.Requests(), 2)                            // two attempts
	assert.Equal(t, uploadTestData, streamClientMock.Requests()[1].GetData()) // make sure the second attempt has the full data
}

func TestClient_UploadStreamToBuildCache_AllAttemptsFail(t *testing.T) {
	streamClientMock := mocks.NewClientStreamClientMock[bytestream.WriteRequest, bytestream.WriteResponse](
		&bytestream.WriteResponse{
			CommittedSize: int64(len(uploadTestData)),
		},
		[]error{errors.New("some error"), errors.New("some error"), errors.New("some error")},
	)

	uploadRetry := uint(2)
	client, err := kv.NewClient(kv.NewClientParams{
		Logger: mockLogger,
		BitriseKVClient: &mocks.KVStorageClientMock{
			PutFunc: func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[bytestream.WriteRequest, bytestream.WriteResponse], error) {
				return streamClientMock, nil
			},
		},
		UploadRetry:     uploadRetry,
		UploadRetryWait: 1, // to make tests faster
	})
	require.NoError(t, err)

	buffer := bytes.NewBuffer(uploadTestData)

	err = client.UploadStreamToBuildCache(context.Background(), buffer, "test-key", int64(buffer.Len()))
	require.Error(t, err)

	assert.Len(t, streamClientMock.Requests(), int(uploadRetry)+1)
}

func TestClient_UploadStreamToBuildCache_NonRetryableError(t *testing.T) {
	streamClientMock := mocks.NewClientStreamClientMock[bytestream.WriteRequest, bytestream.WriteResponse](
		&bytestream.WriteResponse{},
		[]error{status.New(codes.Unauthenticated, "some error").Err()},
	)

	client, err := kv.NewClient(kv.NewClientParams{
		Logger: mockLogger,
		BitriseKVClient: &mocks.KVStorageClientMock{
			PutFunc: func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[bytestream.WriteRequest, bytestream.WriteResponse], error) {
				return streamClientMock, nil
			},
		},
		UploadRetryWait: 1, // to make tests faster
	})
	require.NoError(t, err)

	buffer := bytes.NewBuffer(uploadTestData)

	err = client.UploadStreamToBuildCache(context.Background(), buffer, "test-key", int64(buffer.Len()))
	require.Error(t, err)

	assert.Len(t, streamClientMock.Requests(), 1)
}
