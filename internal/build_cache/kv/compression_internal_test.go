//go:build unit

package kv

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv/mocks"
)

const testIdleTimeout = 50 * time.Millisecond

// scriptedStream plays steps in order, failing like gRPC once the RPC context is done; a nil step blocks until then.
type scriptedStream struct {
	grpc.ServerStreamingClient[bytestream.ReadResponse]

	ctx   context.Context //nolint:containedctx
	steps []func() (*bytestream.ReadResponse, error)
	next  int
}

func (s *scriptedStream) Recv() (*bytestream.ReadResponse, error) {
	step := s.steps[s.next]
	s.next++

	if step == nil {
		<-s.ctx.Done()

		return nil, s.ctx.Err()
	}

	resp, err := step()
	if ctxErr := s.ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	return resp, err
}

func (s *scriptedStream) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (s *scriptedStream) Trailer() metadata.MD         { return nil }

func chunk(data []byte, pause time.Duration) func() (*bytestream.ReadResponse, error) {
	return func() (*bytestream.ReadResponse, error) {
		time.Sleep(pause)

		return &bytestream.ReadResponse{Data: data}, nil
	}
}

func eof() (*bytestream.ReadResponse, error) { return nil, io.EOF }

func idleTestClient(t *testing.T, attempts ...[]func() (*bytestream.ReadResponse, error)) (*Client, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32

	client, err := NewClient(NewClientParams{
		Logger: log.NewLogger(),
		BitriseKVClient: &mocks.KVStorageClientMock{
			GetFunc: func(ctx context.Context, _ *bytestream.ReadRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[bytestream.ReadResponse], error) {
				i := int(calls.Add(1)) - 1

				return &scriptedStream{ctx: ctx, steps: attempts[i]}, nil
			},
		},
		Compression:       CompressionParams{Enabled: true},
		DownloadRetryWait: 1,
	})
	require.NoError(t, err)
	client.downloadIdleTimeout = testIdleTimeout

	return client, &calls
}

func zstdFrame(t *testing.T, c *Client, blob []byte) []byte {
	t.Helper()

	return c.compression.enc.EncodeAll(blob, nil)
}

func TestDownloadCompressed_StalledStreamIsAbortedAndRetried(t *testing.T) {
	blob := bytes.Repeat([]byte("blob "), 1000)

	probe, _ := idleTestClient(t)
	frame := zstdFrame(t, probe, blob)
	client, calls := idleTestClient(t,
		[]func() (*bytestream.ReadResponse, error){nil},
		[]func() (*bytestream.ReadResponse, error){chunk(frame, 0), eof},
	)

	started := time.Now()

	var got bytes.Buffer
	require.NoError(t, client.DownloadStream(context.Background(), &got, "key"))

	assert.Equal(t, blob, got.Bytes())
	assert.EqualValues(t, 2, calls.Load())
	assert.GreaterOrEqual(t, time.Since(started), testIdleTimeout)
}

// The total time exceeds the idle window; only the gaps between chunks are bounded by it.
func TestDownloadCompressed_SlowButProgressingStreamCompletes(t *testing.T) {
	blob := bytes.Repeat([]byte("blob "), 1000)

	probe, _ := idleTestClient(t)
	frame := zstdFrame(t, probe, blob)

	const parts = 6

	gap := testIdleTimeout * 2 / 3
	step := (len(frame) + parts - 1) / parts

	var steps []func() (*bytestream.ReadResponse, error)
	for off := 0; off < len(frame); off += step {
		steps = append(steps, chunk(frame[off:min(off+step, len(frame))], gap))
	}

	steps = append(steps, eof)

	client, calls := idleTestClient(t, steps)

	started := time.Now()

	var got bytes.Buffer
	require.NoError(t, client.DownloadStream(context.Background(), &got, "key"))

	assert.Equal(t, blob, got.Bytes())
	assert.EqualValues(t, 1, calls.Load())
	assert.Greater(t, time.Since(started), testIdleTimeout)
}

func TestCompressedPayload_FromPastTheBlobFails(t *testing.T) {
	c, err := newCompression(CompressionParams{Enabled: true})
	require.NoError(t, err)

	blob := bytes.Repeat([]byte("blob "), 100)
	payload, ok, err := c.compress(bytes.NewReader(blob), int64(len(blob)))
	require.NoError(t, err)
	require.True(t, ok)

	_, _, err = payload.from(int64(len(blob)))

	require.ErrorContains(t, err, "past the blob")
}
