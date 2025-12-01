package kv_test

import (
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
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/slicebuf"
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

	buffer := slicebuf.NewBufferWithData(uploadTestData)

	err = client.UploadStreamToBuildCache(context.Background(), buffer, "test-key", int64(buffer.Len()))
	require.NoError(t, err)
}

func TestClient_UploadStreamToBuildCache_FirstAttemptFails(t *testing.T) {
	streamClientMock := mocks.NewClientStreamClientMock[bytestream.WriteRequest, bytestream.WriteResponse](
		&bytestream.WriteResponse{
			CommittedSize: int64(len(uploadTestData) - 5),
		},
		[]error{errors.New("some error"), nil},
	)

	bitriseKVClient := &mocks.KVStorageClientMock{
		PutFunc: func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[bytestream.WriteRequest, bytestream.WriteResponse], error) {
			return streamClientMock, nil
		},
		WriteStatusFunc: func(ctx context.Context, in *bytestream.QueryWriteStatusRequest, opts ...grpc.CallOption) (*bytestream.QueryWriteStatusResponse, error) {
			return &bytestream.QueryWriteStatusResponse{
				CommittedSize: 5,
				Complete:      false,
			}, nil
		},
	}
	client, err := kv.NewClient(kv.NewClientParams{
		Logger:          mockLogger,
		BitriseKVClient: bitriseKVClient,
		UploadRetryWait: 1, // to make tests faster
	})
	require.NoError(t, err)

	buffer := slicebuf.NewBuffer()
	_, err = buffer.Write(uploadTestData)
	require.NoError(t, err)
	_, err = buffer.Seek(0, io.SeekStart)
	require.NoError(t, err)

	err = client.UploadStreamToBuildCache(context.Background(), buffer, "test-key", int64(buffer.Len()))
	require.NoError(t, err) // should succeed after retry

	require.Len(t, streamClientMock.Requests(), 2)
	require.Equal(t, int64(5), streamClientMock.Requests()[1].GetWriteOffset())
	require.Len(t, bitriseKVClient.WriteStatusCalls(), 1)
	assert.Equal(t, "kv/test-key", bitriseKVClient.WriteStatusCalls()[0].In.GetResourceName())
	assert.Equal(t, uploadTestData[5:], streamClientMock.Requests()[1].GetData()) // make sure the second attempt has offset data
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

	buffer := slicebuf.NewBufferWithData(uploadTestData)

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

	buffer := slicebuf.NewBufferWithData(uploadTestData)

	err = client.UploadStreamToBuildCache(context.Background(), buffer, "test-key", int64(buffer.Len()))
	require.Error(t, err)

	assert.Len(t, streamClientMock.Requests(), 1)
}
