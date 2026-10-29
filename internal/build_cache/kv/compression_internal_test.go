//go:build unit

package kv

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/klauspost/compress/zstd"
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

	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = enc.Close() })

	return enc.EncodeAll(blob, nil)
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
	assert.Less(t, time.Since(started), 5*time.Second, "the idle timer, not the caller's deadline, ended the stalled attempt")
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

	// The pauses let an immediately firing timer (no guard) win deterministically against the stream.
	rig := newIdleTestRig(t, []streamStep{{data: zstdFrame(t, blob), pause: testChunkGap / 2}, {end: io.EOF}})
	rig.client.downloadIdleTimeout = 0

	var got bytes.Buffer
	require.NoError(t, rig.client.DownloadStream(failFastContext(t), &got, "key"))

	assert.Equal(t, blob, got.Bytes())
}

func TestCompressedPayload_FromPastTheBlobFails(t *testing.T) {
	c := newCompression(CompressionParams{Enabled: true})

	blob := bytes.Repeat([]byte("blob "), 100)
	payload, ok, err := c.compress(bytes.NewReader(blob), int64(len(blob)))
	require.NoError(t, err)
	require.True(t, ok)

	_, _, err = payload.from(int64(len(blob)))

	require.ErrorContains(t, err, "past the blob")
}

func decodeAll(t *testing.T, frame []byte) []byte {
	t.Helper()

	dec, err := zstd.NewReader(nil)
	require.NoError(t, err)
	defer dec.Close()

	out, err := dec.DecodeAll(frame, nil)
	require.NoError(t, err)

	return out
}

// A resumed attempt sends the tail as its own frame, which the server decodes and appends at the committed offset.
func TestCompressedPayload_FromReencodesTheTail(t *testing.T) {
	c := newCompression(CompressionParams{Enabled: true})

	blob := bytes.Repeat([]byte("compilation cache blob "), 5000)
	payload, ok, err := c.compress(bytes.NewReader(blob), int64(len(blob)))
	require.NoError(t, err)
	require.True(t, ok)

	assert.Equal(t, blob, decodeAll(t, payload.full))

	const offset = 12_345

	tail, sendLen, err := payload.from(offset)
	require.NoError(t, err)

	frame, err := io.ReadAll(tail)
	require.NoError(t, err)

	assert.EqualValues(t, len(frame), sendLen)
	assert.Equal(t, blob[offset:], decodeAll(t, frame))
}

func TestCompression_ShortSourceIsAnError(t *testing.T) {
	c := newCompression(CompressionParams{Enabled: true})

	blob := bytes.Repeat([]byte("blob "), 100)

	_, _, err := c.compress(bytes.NewReader(blob), int64(len(blob))+50)

	require.ErrorContains(t, err, "read blob")
}

// Coders are reused across transfers and must stay correct when many run at once and when the free lists overflow.
func TestCompression_CodersAreReusableUnderConcurrency(t *testing.T) {
	c := newCompression(CompressionParams{Enabled: true})

	const workers, rounds = 24, 8

	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for r := range rounds {
				blob := bytes.Repeat([]byte{byte(i), byte(r), 'x'}, 3000+i*100+r)

				payload, ok, err := c.compress(bytes.NewReader(blob), int64(len(blob)))
				if err != nil || !ok {
					errs <- fmt.Errorf("worker %d round %d: compress ok=%v err=%w", i, r, ok, err)

					return
				}

				var got bytes.Buffer
				if _, err := c.decodeTo(&got, payload.full); err != nil {
					errs <- fmt.Errorf("worker %d round %d: decode: %w", i, r, err)

					return
				}

				if !bytes.Equal(blob, got.Bytes()) {
					errs <- fmt.Errorf("worker %d round %d: round trip differs", i, r)

					return
				}
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	c.close()
	c.close()
}

func sumHex(b []byte) string {
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:])
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()

	b := make([]byte, n)
	_, err := rand.Read(b)
	require.NoError(t, err)

	return b
}

func TestDecodeInto_SmallBlobIsWrittenWhileHashed(t *testing.T) {
	c := newCompression(CompressionParams{Enabled: true})

	blob := bytes.Repeat([]byte("blob "), 1000)
	frame := zstdFrame(t, blob)
	require.Less(t, len(frame), twoPassDecodeMin)

	var dest bytes.Buffer
	n, err := c.decodeInto(&dest, frame, func(decoded int64, sum string) error {
		assert.EqualValues(t, len(blob), decoded)
		assert.Equal(t, sumHex(blob), sum)

		return nil
	})

	require.NoError(t, err)
	assert.EqualValues(t, len(blob), n)
	assert.Equal(t, blob, dest.Bytes())
}

// Incompressible data keeps the frame as large as the blob, which puts it on the two-pass path.
func TestDecodeInto_LargeBlobIsVerifiedBeforeAnythingIsWritten(t *testing.T) {
	c := newCompression(CompressionParams{Enabled: true})

	blob := randomBytes(t, twoPassDecodeMin+4096)
	frame := zstdFrame(t, blob)
	require.GreaterOrEqual(t, len(frame), twoPassDecodeMin)

	t.Run("accepted", func(t *testing.T) {
		var dest bytes.Buffer
		n, err := c.decodeInto(&dest, frame, func(decoded int64, sum string) error {
			assert.EqualValues(t, len(blob), decoded)
			assert.Equal(t, sumHex(blob), sum)

			return nil
		})

		require.NoError(t, err)
		assert.EqualValues(t, len(blob), n)
		assert.Equal(t, blob, dest.Bytes())
		assert.Less(t, dest.Cap(), len(blob)+len(blob)/4, "sized from the first pass, not grown by doubling")
	})

	t.Run("rejected", func(t *testing.T) {
		var dest bytes.Buffer
		_, err := c.decodeInto(&dest, frame, func(int64, string) error { return errors.New("hash mismatch") })

		require.ErrorContains(t, err, "hash mismatch")
		assert.Zero(t, dest.Len(), "nothing is written for a blob that failed verification")
	})
}

func TestDecodeInto_CorruptFrameWritesNothing(t *testing.T) {
	c := newCompression(CompressionParams{Enabled: true})

	var dest bytes.Buffer
	_, err := c.decodeInto(&dest, []byte("not a zstd frame"), func(int64, string) error { return nil })

	require.ErrorContains(t, err, "zstd decode")
	assert.Zero(t, dest.Len())
}
