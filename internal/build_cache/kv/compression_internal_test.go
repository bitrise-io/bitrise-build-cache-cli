//go:build unit

package kv

import (
	"bytes"
	"context"
	"io"
	"sync"
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

// Wide margins: the gaps below must stay well under the window even when a loaded CI runner oversleeps.
const (
	testIdleTimeout = 300 * time.Millisecond
	testChunkGap    = 100 * time.Millisecond
)

// streamStep is one Recv of a scripted stream: wait pause (or until the RPC is cancelled), then deliver data or end.
type streamStep struct {
	data  []byte
	pause time.Duration
	block bool
	end   error
}

// scriptedStream plays steps in order and, like a gRPC stream, returns as soon as the RPC context is done.
type scriptedStream struct {
	grpc.ServerStreamingClient[bytestream.ReadResponse]

	ctx   context.Context //nolint:containedctx
	steps []streamStep
	next  int
}

func (s *scriptedStream) Recv() (*bytestream.ReadResponse, error) {
	step := s.steps[s.next]
	s.next++

	if step.block {
		<-s.ctx.Done()

		return nil, s.ctx.Err()
	}

	select {
	case <-time.After(step.pause):
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}

	if step.end != nil {
		return nil, step.end
	}

	return &bytestream.ReadResponse{Data: step.data}, nil
}

func (s *scriptedStream) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (s *scriptedStream) Trailer() metadata.MD         { return nil }

type getRequest struct {
	resource string
	offset   int64
}

type idleTestRig struct {
	client *Client

	mu       sync.Mutex
	requests []getRequest
}

func (r *idleTestRig) recorded() []getRequest {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]getRequest(nil), r.requests...)
}

func newIdleTestRig(t *testing.T, attempts ...[]streamStep) *idleTestRig {
	t.Helper()

	rig := &idleTestRig{}

	client, err := NewClient(NewClientParams{
		Logger: log.NewLogger(),
		BitriseKVClient: &mocks.KVStorageClientMock{
			GetFunc: func(ctx context.Context, in *bytestream.ReadRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[bytestream.ReadResponse], error) {
				rig.mu.Lock()
				i := len(rig.requests)
				rig.requests = append(rig.requests, getRequest{resource: in.GetResourceName(), offset: in.GetReadOffset()})
				rig.mu.Unlock()

				return &scriptedStream{ctx: ctx, steps: attempts[i]}, nil
			},
		},
		Compression:       CompressionParams{Enabled: true},
		DownloadRetryWait: 1,
	})
	require.NoError(t, err)
	client.downloadIdleTimeout = testIdleTimeout

	rig.client = client

	return rig
}

func zstdFrame(t *testing.T, blob []byte) []byte {
	t.Helper()

	c, err := newCompression(CompressionParams{Enabled: true})
	require.NoError(t, err)

	return c.enc.EncodeAll(blob, nil)
}

// A caller-side deadline turns a regression (timer never fires) into a quick failure instead of a hung test run.
func failFastContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	return ctx
}

func TestDownloadCompressed_StalledStreamIsAbortedAndRetried(t *testing.T) {
	blob := bytes.Repeat([]byte("blob "), 1000)
	frame := zstdFrame(t, blob)

	rig := newIdleTestRig(t,
		[]streamStep{{block: true}},
		[]streamStep{{data: frame}, {end: io.EOF}},
	)

	started := time.Now()

	var got bytes.Buffer
	require.NoError(t, rig.client.DownloadStream(failFastContext(t), &got, "key"))

	assert.Equal(t, blob, got.Bytes())
	assert.Equal(t, []getRequest{{resource: "kv/zstd/key"}, {resource: "kv/zstd/key"}}, rig.recorded(), "both attempts start from offset 0")
	assert.GreaterOrEqual(t, time.Since(started), testIdleTimeout)
}

// The total time exceeds the idle window; only the gaps between chunks are bounded by it.
func TestDownloadCompressed_SlowButProgressingStreamCompletes(t *testing.T) {
	blob := bytes.Repeat([]byte("blob "), 1000)
	frame := zstdFrame(t, blob)

	const parts = 5

	step := (len(frame) + parts - 1) / parts

	var steps []streamStep
	for off := 0; off < len(frame); off += step {
		steps = append(steps, streamStep{data: frame[off:min(off+step, len(frame))], pause: testChunkGap})
	}

	steps = append(steps, streamStep{end: io.EOF})

	rig := newIdleTestRig(t, steps)

	started := time.Now()

	var got bytes.Buffer
	require.NoError(t, rig.client.DownloadStream(failFastContext(t), &got, "key"))

	assert.Equal(t, blob, got.Bytes())
	assert.Equal(t, []getRequest{{resource: "kv/zstd/key"}}, rig.recorded())
	assert.Greater(t, time.Since(started), testIdleTimeout)
}

func TestDownloadCompressed_ZeroIdleWindowFallsBackToTheDefault(t *testing.T) {
	blob := bytes.Repeat([]byte("blob "), 100)

	rig := newIdleTestRig(t, []streamStep{{data: zstdFrame(t, blob)}, {end: io.EOF}})
	rig.client.downloadIdleTimeout = 0

	var got bytes.Buffer
	require.NoError(t, rig.client.DownloadStream(failFastContext(t), &got, "key"))

	assert.Equal(t, blob, got.Bytes())
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
