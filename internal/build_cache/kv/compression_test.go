package kv_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv/mocks"
)

const (
	zstdResourcePrefix = "kv/zstd/"
	checksumHeader     = "x-flare-blob-validation-sha256"
)

type putMessage struct {
	offset int64
	finish bool
	size   int
}

type putRecord struct {
	resource string
	offset   int64
	checksum string
	wire     []byte
	messages []putMessage
}

type getRecord struct {
	resource string
	offset   int64
}

// backendStore emulates the build-cache-api KV endpoints: blobs are stored uncompressed under one key whatever
// the form requested, Put decompresses and, for a write starting at offset 0, validates the checksum on the
// uncompressed bytes (a resumed write is not validated), Get compresses on the way out with a header describing
// the uncompressed blob, offsets count uncompressed bytes, and WriteStatus only reports the committed size.
type backendStore struct {
	mu    sync.Mutex
	blobs map[string][]byte
	puts  []putRecord
	gets  []getRecord

	failPutPrefix []byte
	failPutStatus error
	failGetAfter  int
	getOverride   map[string][]byte
	headerValue   string

	decoder *zstd.Decoder
	encoder *zstd.Encoder
}

func newBackendStore(t *testing.T) *backendStore {
	t.Helper()

	dec, err := zstd.NewReader(nil)
	require.NoError(t, err)
	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	t.Cleanup(func() { dec.Close(); _ = enc.Close() })

	return &backendStore{
		blobs:       map[string][]byte{},
		getOverride: map[string][]byte{},
		decoder:     dec,
		encoder:     enc,
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:])
}

func blobKeyOf(resource string) (string, bool) {
	if name, ok := strings.CutPrefix(resource, zstdResourcePrefix); ok {
		return name, true
	}

	return strings.TrimPrefix(resource, "kv/"), false
}

func (s *backendStore) blob(key string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.blobs[key]
}

func (s *backendStore) putRecords() []putRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]putRecord(nil), s.puts...)
}

func (s *backendStore) getRecords() []getRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]getRecord(nil), s.gets...)
}

// failNextPutAfterCommitting makes the next Put keep prefix (the first bytes of the blob) and then fail.
func (s *backendStore) failNextPutAfterCommitting(prefix []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failPutPrefix = prefix
}

// failNextGetAfter makes the next Get stream n bytes of its response and then break.
func (s *backendStore) failNextGetAfter(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failGetAfter = n
}

// failPutWith makes every Put fail with err after the client has sent its data.
func (s *backendStore) failPutWith(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failPutStatus = err
}

func (s *backendStore) setHeader(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.headerValue = value
}

func (s *backendStore) overrideGet(resource string, response []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.getOverride[resource] = response
}

func (s *backendStore) put(resource string, offset int64, checksum string, wire []byte, messages []putMessage) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.puts = append(s.puts, putRecord{resource: resource, offset: offset, checksum: checksum, wire: wire, messages: messages})

	key, compressed := blobKeyOf(resource)

	if s.failPutStatus != nil {
		return 0, s.failPutStatus
	}

	if s.failPutPrefix != nil {
		s.blobs[key] = s.failPutPrefix
		s.failPutPrefix = nil

		return 0, errors.New("connection reset")
	}

	data := wire
	if compressed {
		var err error
		if data, err = s.decoder.DecodeAll(wire, nil); err != nil {
			return 0, status.Errorf(codes.InvalidArgument, "decompress: %v", err)
		}
	}

	existing := s.blobs[key]
	if offset > int64(len(existing)) {
		return 0, status.Errorf(codes.OutOfRange, "offset %d is past the stored %d bytes", offset, len(existing))
	}

	full := append(append([]byte(nil), existing[:offset]...), data...)
	if offset == 0 {
		if got := sha256Hex(full); got != checksum {
			return 0, status.Errorf(codes.InvalidArgument, "validation failed: expected %s, got %s", checksum, got)
		}
	}

	s.blobs[key] = full

	return int64(len(wire)), nil
}

func (s *backendStore) writeStatus(resource string) *bytestream.QueryWriteStatusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, _ := blobKeyOf(resource)

	return &bytestream.QueryWriteStatusResponse{CommittedSize: int64(len(s.blobs[key]))}
}

func (s *backendStore) get(resource string, offset int64) grpc.ServerStreamingClient[bytestream.ReadResponse] {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.gets = append(s.gets, getRecord{resource: resource, offset: offset})

	key, compressed := blobKeyOf(resource)

	if override, ok := s.getOverride[resource]; ok {
		return responseStream(override, sha256Hex(override), nil)
	}

	blob, found := s.blobs[key]
	if !found {
		return mocks.NewServerStreamingClientMock[bytestream.ReadResponse]([]mocks.RecvResult[bytestream.ReadResponse]{
			{Error: status.Error(codes.NotFound, "not found")},
		})
	}

	header := sha256Hex(blob)
	if s.headerValue != "" {
		header = s.headerValue
	}

	data := blob[offset:]
	if compressed {
		data = s.encoder.EncodeAll(data, nil)
	}

	var breakWith error
	if s.failGetAfter > 0 {
		data, breakWith = data[:s.failGetAfter], errors.New("connection reset")
		s.failGetAfter = 0
	}

	return responseStream(data, header, breakWith)
}

func responseStream(data []byte, header string, breakWith error) grpc.ServerStreamingClient[bytestream.ReadResponse] {
	end := io.EOF
	if breakWith != nil {
		end = breakWith
	}

	return mocks.NewServerStreamingClientMock[bytestream.ReadResponse]([]mocks.RecvResult[bytestream.ReadResponse]{
		{Response: &bytestream.ReadResponse{Data: data}, Metadata: map[string]string{checksumHeader: header}},
		{Error: end},
	})
}

func (s *backendStore) kvMock() *mocks.KVStorageClientMock {
	return &mocks.KVStorageClientMock{
		PutFunc: func(ctx context.Context, _ ...grpc.CallOption) (grpc.ClientStreamingClient[bytestream.WriteRequest, bytestream.WriteResponse], error) {
			md, _ := metadata.FromOutgoingContext(ctx)

			return &storingPutStream{store: s, checksum: strings.Join(md.Get(checksumHeader), ",")}, nil
		},
		GetFunc: func(_ context.Context, in *bytestream.ReadRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[bytestream.ReadResponse], error) {
			return s.get(in.GetResourceName(), in.GetReadOffset()), nil
		},
		WriteStatusFunc: func(_ context.Context, in *bytestream.QueryWriteStatusRequest, _ ...grpc.CallOption) (*bytestream.QueryWriteStatusResponse, error) {
			return s.writeStatus(in.GetResourceName()), nil
		},
	}
}

type storingPutStream struct {
	grpc.ClientStream

	store    *backendStore
	checksum string
	resource string
	offset   int64
	body     []byte
	messages []putMessage
	started  bool
}

func (p *storingPutStream) Send(req *bytestream.WriteRequest) error {
	if !p.started {
		p.resource, p.offset, p.started = req.GetResourceName(), req.GetWriteOffset(), true
	}
	p.body = append(p.body, req.GetData()...)
	p.messages = append(p.messages, putMessage{offset: req.GetWriteOffset(), finish: req.GetFinishWrite(), size: len(req.GetData())})

	return nil
}

func (p *storingPutStream) CloseAndRecv() (*bytestream.WriteResponse, error) {
	committed, err := p.store.put(p.resource, p.offset, p.checksum, p.body, p.messages)
	if err != nil {
		return nil, err
	}

	return &bytestream.WriteResponse{CommittedSize: committed}, nil
}

func (p *storingPutStream) CloseSend() error             { return nil }
func (p *storingPutStream) Header() (metadata.MD, error) { return nil, nil } //nolint:nilnil
func (p *storingPutStream) Trailer() metadata.MD         { return nil }
func (p *storingPutStream) Context() context.Context     { return context.Background() }

func newTestClient(t *testing.T, store *backendStore, params kv.CompressionParams) *kv.Client {
	t.Helper()

	client, err := kv.NewClient(kv.NewClientParams{
		Logger:            mockLogger,
		BitriseKVClient:   store.kvMock(),
		Compression:       params,
		UploadRetryWait:   1,
		DownloadRetryWait: 1,
	})
	require.NoError(t, err)

	return client
}

func compressibleBlob(size int) []byte {
	return bytes.Repeat([]byte("compilation cache blob "), size/23+1)[:size]
}

func randomBlob(t *testing.T, size int) []byte {
	t.Helper()

	blob := make([]byte, size)
	_, err := rand.Read(blob)
	require.NoError(t, err)

	return blob
}

func upload(t *testing.T, client *kv.Client, key string, blob []byte) {
	t.Helper()

	require.NoError(t, client.UploadStreamToBuildCache(context.Background(), bytes.NewReader(blob), key, int64(len(blob))))
}

func download(t *testing.T, client *kv.Client, key string) []byte {
	t.Helper()

	var got bytes.Buffer
	require.NoError(t, client.DownloadStream(context.Background(), &got, key))

	return got.Bytes()
}

func TestClient_Compression_RoundTrip(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true, Threshold: 100})

	blob := compressibleBlob(64 * 1024)
	upload(t, client, "key", blob)

	puts := store.putRecords()
	require.Len(t, puts, 1)
	assert.Equal(t, "kv/zstd/key", puts[0].resource)
	assert.Less(t, len(puts[0].wire), len(blob)/4)
	assert.Equal(t, blob, store.blob("key"))

	assert.Equal(t, blob, download(t, client, "key"))
	assert.Equal(t, []getRecord{{resource: "kv/zstd/key"}}, store.getRecords())
}

func TestClient_Compression_SharedWithUncompressedClients(t *testing.T) {
	store := newBackendStore(t)
	compressing := newTestClient(t, store, kv.CompressionParams{Enabled: true})
	plain := newTestClient(t, store, kv.CompressionParams{})

	fromCompressing := compressibleBlob(8 * 1024)
	upload(t, compressing, "a", fromCompressing)
	assert.Equal(t, fromCompressing, download(t, plain, "a"))

	fromPlain := compressibleBlob(9 * 1024)
	upload(t, plain, "b", fromPlain)
	assert.Equal(t, fromPlain, download(t, compressing, "b"))
}

func TestClient_Compression_BelowThresholdIsSentAsIs(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true, Threshold: 1024})

	blob := compressibleBlob(512)
	upload(t, client, "small", blob)

	puts := store.putRecords()
	require.Len(t, puts, 1)
	assert.Equal(t, "kv/small", puts[0].resource)
	assert.Equal(t, blob, puts[0].wire)

	assert.Equal(t, blob, download(t, client, "small"))
}

func TestClient_Compression_IncompressibleIsSentAsIs(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true, Threshold: 100})

	blob := randomBlob(t, 4096)
	upload(t, client, "random", blob)

	puts := store.putRecords()
	require.Len(t, puts, 1)
	assert.Equal(t, "kv/random", puts[0].resource)
	assert.Equal(t, blob, download(t, client, "random"))
}

// uploadStream treats size 0 as already committed, so an empty blob is never written, compressed or not.
func TestClient_Compression_EmptyBlobStoresNothing(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	require.NoError(t, client.UploadStreamToBuildCache(context.Background(), bytes.NewReader(nil), "empty", 0))

	assert.Empty(t, store.putRecords())
}

func TestClient_Compression_DisabledKeepsPlainNames(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{})

	blob := compressibleBlob(64 * 1024)
	upload(t, client, "key", blob)

	assert.Equal(t, blob, download(t, client, "key"))
	assert.Equal(t, "kv/key", store.putRecords()[0].resource)
	assert.Equal(t, []getRecord{{resource: "kv/key"}}, store.getRecords())
}

func TestClient_Compression_Gate(t *testing.T) {
	const threshold = 1024

	tests := []struct {
		name    string
		enabled bool
		size    int
		want    string
	}{
		{"disabled below", false, threshold - 1, "kv/key"},
		{"disabled at", false, threshold, "kv/key"},
		{"disabled above", false, threshold + 1, "kv/key"},
		{"enabled below", true, threshold - 1, "kv/key"},
		{"enabled at", true, threshold, "kv/zstd/key"},
		{"enabled above", true, threshold + 1, "kv/zstd/key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newBackendStore(t)
			client := newTestClient(t, store, kv.CompressionParams{Enabled: tt.enabled, Threshold: threshold})
			blob := compressibleBlob(tt.size)

			upload(t, client, "key", blob)

			require.Len(t, store.putRecords(), 1)
			assert.Equal(t, tt.want, store.putRecords()[0].resource)
			assert.Equal(t, blob, download(t, client, "key"))
		})
	}
}

func TestClient_Compression_ZeroThresholdMeansDefault(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	upload(t, client, "below", compressibleBlob(int(kv.DefaultCompressionThreshold)-1))
	upload(t, client, "at", compressibleBlob(int(kv.DefaultCompressionThreshold)))

	puts := store.putRecords()
	require.Len(t, puts, 2)
	assert.Equal(t, "kv/below", puts[0].resource)
	assert.Equal(t, "kv/zstd/at", puts[1].resource)
}

// The server validates the checksum on the uncompressed blob, so the header must hash the raw bytes.
func TestClient_Compression_UploadChecksumIsOverRawBytes(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	blob := compressibleBlob(64 * 1024)
	upload(t, client, "key", blob)

	put := store.putRecords()[0]
	assert.Equal(t, sha256Hex(blob), put.checksum)
	assert.NotEqual(t, sha256Hex(put.wire), put.checksum)
}

// The server's hash header describes the uncompressed blob, so a wrong header must fail the download.
func TestClient_Compression_DownloadChecksIntegrityAfterDecoding(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	upload(t, client, "key", compressibleBlob(64*1024))
	store.setHeader(strings.Repeat("0", 64))

	var got bytes.Buffer
	err := client.DownloadStream(context.Background(), &got, "key")

	require.ErrorContains(t, err, "hash mismatch")
}

func TestClient_Compression_DownloadRejectsCorruptFrame(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	store.overrideGet("kv/zstd/key", []byte("not a zstd frame"))

	var got bytes.Buffer
	err := client.DownloadStream(context.Background(), &got, "key")

	require.ErrorContains(t, err, "zstd decode")
	assert.Empty(t, got.Bytes())
}

func TestClient_Compression_DownloadMissIsErrCacheNotFound(t *testing.T) {
	client := newTestClient(t, newBackendStore(t), kv.CompressionParams{Enabled: true})

	err := client.DownloadStream(context.Background(), io.Discard, "absent")

	require.ErrorIs(t, err, kv.ErrCacheNotFound)
}

// Write offsets count uncompressed bytes: a retry resumes the blob at the committed offset, not the compressed one.
func TestClient_Compression_UploadResumesAtUncompressedOffset(t *testing.T) {
	const committed = 100_000

	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	// Compresses to far fewer bytes than are already committed, which a compressed-offset client would take for "done".
	blob := compressibleBlob(300_000)
	store.failNextPutAfterCommitting(blob[:committed])

	upload(t, client, "key", blob)

	puts := store.putRecords()
	require.Len(t, puts, 2)
	assert.Equal(t, "kv/zstd/key", puts[0].resource)
	assert.EqualValues(t, 0, puts[0].offset)
	assert.Equal(t, "kv/zstd/key", puts[1].resource)
	assert.EqualValues(t, committed, puts[1].offset)
	assert.Equal(t, sha256Hex(blob), puts[1].checksum)
	assert.Equal(t, blob, store.blob("key"))
}

// FinishWrite is derived from PutParams.FileSize, which for a compressed Put is the compressed length: sizing it
// by the raw blob would leave the stream unfinished.
func TestClient_Compression_UploadFinishesTheStream(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	upload(t, client, "key", compressibleBlob(300_000))

	messages := store.putRecords()[0].messages
	require.NotEmpty(t, messages)
	assert.True(t, messages[len(messages)-1].finish)
}

func TestClient_Compression_ResumedUploadFinishesTheStream(t *testing.T) {
	const committed = 100_000

	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	blob := compressibleBlob(300_000)
	store.failNextPutAfterCommitting(blob[:committed])
	upload(t, client, "key", blob)

	puts := store.putRecords()
	require.Len(t, puts, 2)

	messages := puts[1].messages
	require.NotEmpty(t, messages)
	assert.EqualValues(t, committed, messages[0].offset)
	assert.True(t, messages[len(messages)-1].finish)
	assert.Equal(t, blob, store.blob("key"))
}

func TestClient_Compression_UploadAbortsOnUnauthenticated(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})
	store.failPutWith(status.Error(codes.Unauthenticated, "token rejected"))

	blob := compressibleBlob(64 * 1024)
	err := client.UploadStreamToBuildCache(context.Background(), bytes.NewReader(blob), "key", int64(len(blob)))

	require.ErrorIs(t, err, kv.ErrCacheUnauthenticated)
	assert.Len(t, store.putRecords(), 1)
}

// Read offsets count uncompressed bytes, so a broken download starts over instead of resuming mid-frame.
func TestClient_Compression_DownloadRestartsFromTheStart(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	blob := compressibleBlob(100_000)
	upload(t, client, "key", blob)
	store.failNextGetAfter(10)

	assert.Equal(t, blob, download(t, client, "key"))

	assert.Equal(t, []getRecord{{resource: "kv/zstd/key"}, {resource: "kv/zstd/key"}}, store.getRecords())
}

func TestClient_Compression_Concurrent(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	const workers = 16

	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			key := fmt.Sprintf("key-%d", i)
			blob := compressibleBlob(2000 + i*500)

			if err := client.UploadStreamToBuildCache(context.Background(), bytes.NewReader(blob), key, int64(len(blob))); err != nil {
				errs <- fmt.Errorf("upload %s: %w", key, err)

				return
			}

			var got bytes.Buffer
			if err := client.DownloadStream(context.Background(), &got, key); err != nil {
				errs <- fmt.Errorf("download %s: %w", key, err)

				return
			}

			if !bytes.Equal(blob, got.Bytes()) {
				errs <- fmt.Errorf("%s: round trip differs", key)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

func TestClient_Compression_CloseIsIdempotent(t *testing.T) {
	client := newTestClient(t, newBackendStore(t), kv.CompressionParams{Enabled: true})

	require.NoError(t, client.Close())
	require.NoError(t, client.Close())
}

// An incompressible blob is sent as is and comes back as a frame as large as itself, which takes the two-pass decode.
func TestClient_Compression_LargeIncompressibleBlobRoundTrips(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	blob := randomBlob(t, 1536*1024)
	upload(t, client, "big", blob)

	assert.Equal(t, "kv/big", store.putRecords()[0].resource)
	assert.Equal(t, blob, download(t, client, "big"))
}

func TestClient_Compression_LargeBlobWithWrongHashLeavesDestinationEmpty(t *testing.T) {
	store := newBackendStore(t)
	client := newTestClient(t, store, kv.CompressionParams{Enabled: true})

	upload(t, client, "big", randomBlob(t, 1536*1024))
	store.setHeader(strings.Repeat("0", 64))

	var got bytes.Buffer
	err := client.DownloadStream(context.Background(), &got, "big")

	require.ErrorContains(t, err, "hash mismatch")
	assert.Zero(t, got.Len())
}
