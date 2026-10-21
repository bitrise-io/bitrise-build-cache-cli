package kv_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

// memoryStore keeps blobs by resource name, so a test sees exactly what was sent and can read it back.
// With converting set it behaves like the backend: it keeps raw bytes per logical key and
// serves or accepts either form, so kv/zstd/<k> and kv/<k> address the same blob.
type memoryStore struct {
	mu         sync.Mutex
	blobs      map[string][]byte
	checksums  map[string]string
	requested  []string
	converting bool
}

var (
	testZstdEncoder, _ = zstd.NewWriter(nil) //nolint:gochecknoglobals
	testZstdDecoder, _ = zstd.NewReader(nil) //nolint:gochecknoglobals
)

func newMemoryStore() *memoryStore {
	return &memoryStore{blobs: map[string][]byte{}, checksums: map[string]string{}}
}

func newConvertingStore() *memoryStore {
	s := newMemoryStore()
	s.converting = true

	return s
}

func (s *memoryStore) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.blobs))
	for name := range s.blobs {
		names = append(names, name)
	}

	return names
}

func (s *memoryStore) requestedNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.requested...)
}

func (s *memoryStore) blob(name string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.blobs[name]
}

func (s *memoryStore) put(name, checksum string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.checksums[name] = checksum

	logical, compressed := strings.CutPrefix(name, "kv/zstd/")
	if !s.converting || !compressed {
		s.blobs[name] = body

		return nil
	}

	raw, err := testZstdDecoder.DecodeAll(body, nil)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	s.blobs["kv/"+logical] = raw

	return nil
}

func (s *memoryStore) get(name string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requested = append(s.requested, name)

	logical, compressed := strings.CutPrefix(name, "kv/zstd/")
	if !s.converting || !compressed {
		data, found := s.blobs[name]

		return data, found
	}

	raw, found := s.blobs["kv/"+logical]
	if !found {
		return nil, false
	}

	return testZstdEncoder.EncodeAll(raw, nil), true
}

func (s *memoryStore) kvMock() *mocks.KVStorageClientMock {
	return &mocks.KVStorageClientMock{
		PutFunc: func(ctx context.Context, _ ...grpc.CallOption) (grpc.ClientStreamingClient[bytestream.WriteRequest, bytestream.WriteResponse], error) {
			md, _ := metadata.FromOutgoingContext(ctx)

			return &storingPutStream{store: s, checksum: strings.Join(md.Get("x-flare-blob-validation-sha256"), ",")}, nil
		},
		GetFunc: func(_ context.Context, in *bytestream.ReadRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[bytestream.ReadResponse], error) {
			data, found := s.get(in.GetResourceName())
			if !found {
				return mocks.NewServerStreamingClientMock[bytestream.ReadResponse]([]mocks.RecvResult[bytestream.ReadResponse]{
					{Error: status.Error(codes.NotFound, "not found")},
				}), nil
			}

			return mocks.NewServerStreamingClientMock[bytestream.ReadResponse]([]mocks.RecvResult[bytestream.ReadResponse]{
				{Response: &bytestream.ReadResponse{Data: data}},
				{Error: io.EOF},
			}), nil
		},
	}
}

type storingPutStream struct {
	grpc.ClientStream

	store    *memoryStore
	checksum string
	name     string
	body     []byte
}

func (p *storingPutStream) Send(req *bytestream.WriteRequest) error {
	p.name = req.GetResourceName()
	p.body = append(p.body, req.GetData()...)

	return nil
}

func (p *storingPutStream) CloseAndRecv() (*bytestream.WriteResponse, error) {
	if err := p.store.put(p.name, p.checksum, p.body); err != nil {
		return nil, err
	}

	return &bytestream.WriteResponse{CommittedSize: int64(len(p.body))}, nil
}

func (p *storingPutStream) CloseSend() error             { return nil }
func (p *storingPutStream) Header() (metadata.MD, error) { return nil, nil } //nolint:nilnil
func (p *storingPutStream) Trailer() metadata.MD         { return nil }
func (p *storingPutStream) Context() context.Context     { return context.Background() }

func newCompressionTestClient(t *testing.T, store *memoryStore, params kv.CompressionParams) *kv.Client {
	t.Helper()

	client, err := kv.NewClient(kv.NewClientParams{
		Logger:          mockLogger,
		BitriseKVClient: store.kvMock(),
		Compression:     params,
	})
	require.NoError(t, err)

	return client
}

func compressibleBlob(size int) []byte {
	return bytes.Repeat([]byte("compilation cache blob "), size/23+1)[:size]
}

func upload(t *testing.T, client *kv.Client, key string, blob []byte) {
	t.Helper()

	require.NoError(t, client.UploadStreamToBuildCache(context.Background(), bytes.NewReader(blob), key, int64(len(blob))))
}

func TestClient_Compression_RoundTrip(t *testing.T) {
	store := newMemoryStore()
	client := newCompressionTestClient(t, store, kv.CompressionParams{Enabled: true, Threshold: 100})

	blob := compressibleBlob(64 * 1024)
	upload(t, client, "key", blob)

	assert.Equal(t, []string{"kv/zstd/key"}, store.names())
	assert.Less(t, len(store.blob("kv/zstd/key")), len(blob)/4)

	var got bytes.Buffer
	require.NoError(t, client.DownloadStream(context.Background(), &got, "key"))

	assert.Equal(t, blob, got.Bytes())
	assert.Equal(t, []string{"kv/zstd/key"}, store.requestedNames())
}

func TestClient_Compression_BelowThresholdUploadsRaw(t *testing.T) {
	store := newMemoryStore()
	client := newCompressionTestClient(t, store, kv.CompressionParams{Enabled: true, Threshold: 1024})

	blob := compressibleBlob(512)
	upload(t, client, "small", blob)

	assert.Equal(t, []string{"kv/small"}, store.names())
	assert.Equal(t, blob, store.blob("kv/small"))
}

func TestClient_Compression_IncompressibleUploadsRaw(t *testing.T) {
	store := newMemoryStore()
	client := newCompressionTestClient(t, store, kv.CompressionParams{Enabled: true, Threshold: 100})

	blob := make([]byte, 4096)
	_, err := rand.Read(blob)
	require.NoError(t, err)
	upload(t, client, "random", blob)

	assert.Equal(t, []string{"kv/random"}, store.names())
	assert.Equal(t, blob, store.blob("kv/random"))
}

// An empty blob is never written (uploadStream treats size 0 as already committed), compression or not.
func TestClient_Compression_EmptyBlobStoresNothing(t *testing.T) {
	store := newMemoryStore()
	client := newCompressionTestClient(t, store, kv.CompressionParams{Enabled: true})

	require.NoError(t, client.UploadStreamToBuildCache(context.Background(), bytes.NewReader(nil), "empty", 0))

	assert.Empty(t, store.names())
}

func TestClient_Compression_DisabledKeepsPlainNames(t *testing.T) {
	store := newMemoryStore()
	client := newCompressionTestClient(t, store, kv.CompressionParams{})

	blob := compressibleBlob(64 * 1024)
	upload(t, client, "key", blob)

	assert.Equal(t, []string{"kv/key"}, store.names())
	assert.Equal(t, blob, store.blob("kv/key"))

	var got bytes.Buffer
	require.NoError(t, client.DownloadStream(context.Background(), &got, "key"))

	assert.Equal(t, blob, got.Bytes())
	assert.Equal(t, []string{"kv/key"}, store.requestedNames())
}

func TestClient_Compression_DownloadRejectsCorruptFrame(t *testing.T) {
	store := newMemoryStore()
	client := newCompressionTestClient(t, store, kv.CompressionParams{Enabled: true})

	require.NoError(t, store.put("kv/zstd/key", "", []byte("not a zstd frame")))

	var got bytes.Buffer
	err := client.DownloadStream(context.Background(), &got, "key")

	require.ErrorContains(t, err, "zstd decode")
	assert.Empty(t, got.Bytes())
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
			store := newMemoryStore()
			client := newCompressionTestClient(t, store, kv.CompressionParams{Enabled: tt.enabled, Threshold: threshold})

			upload(t, client, "key", compressibleBlob(tt.size))

			assert.Equal(t, []string{tt.want}, store.names())
		})
	}
}

func TestClient_Compression_ZeroThresholdMeansDefault(t *testing.T) {
	store := newMemoryStore()
	client := newCompressionTestClient(t, store, kv.CompressionParams{Enabled: true})

	upload(t, client, "below", compressibleBlob(int(kv.DefaultCompressionThreshold)-1))
	upload(t, client, "at", compressibleBlob(int(kv.DefaultCompressionThreshold)))

	assert.ElementsMatch(t, []string{"kv/below", "kv/zstd/at"}, store.names())
}

func TestClient_Compression_DownloadMissIsErrCacheNotFound(t *testing.T) {
	client := newCompressionTestClient(t, newMemoryStore(), kv.CompressionParams{Enabled: true})

	err := client.DownloadStream(context.Background(), io.Discard, "absent")

	require.ErrorIs(t, err, kv.ErrCacheNotFound)
}

// The backend validates the upload against the stored bytes, so the header must hash the zstd form.
func TestClient_Compression_UploadChecksumCoversStoredBytes(t *testing.T) {
	store := newMemoryStore()
	client := newCompressionTestClient(t, store, kv.CompressionParams{Enabled: true})

	blob := compressibleBlob(64 * 1024)
	upload(t, client, "key", blob)

	stored := sha256.Sum256(store.blob("kv/zstd/key"))
	raw := sha256.Sum256(blob)

	assert.Equal(t, hex.EncodeToString(stored[:]), store.checksums["kv/zstd/key"])
	assert.NotEqual(t, hex.EncodeToString(raw[:]), store.checksums["kv/zstd/key"])
}

// Pins the symmetry assumption: a blob stored raw is still readable through the zstd name.
func TestClient_Compression_SmallBlobReadableThroughZstdName(t *testing.T) {
	store := newConvertingStore()
	client := newCompressionTestClient(t, store, kv.CompressionParams{Enabled: true, Threshold: 1024})

	blob := compressibleBlob(512)
	upload(t, client, "small", blob)
	require.Equal(t, []string{"kv/small"}, store.names())

	var got bytes.Buffer
	require.NoError(t, client.DownloadStream(context.Background(), &got, "small"))

	assert.Equal(t, blob, got.Bytes())
	assert.Equal(t, []string{"kv/zstd/small"}, store.requestedNames())
}

func TestClient_Compression_Concurrent(t *testing.T) {
	store := newMemoryStore()
	client := newCompressionTestClient(t, store, kv.CompressionParams{Enabled: true})

	const workers = 16

	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			key := fmt.Sprintf("key-%d", i)
			blob := bytes.Repeat([]byte(key), 4096)

			if err := client.UploadStreamToBuildCache(context.Background(), bytes.NewReader(blob), key, int64(len(blob))); err != nil {
				errs <- err

				return
			}

			var got bytes.Buffer
			if err := client.DownloadStream(context.Background(), &got, key); err != nil {
				errs <- err

				return
			}

			if !bytes.Equal(blob, got.Bytes()) {
				errs <- fmt.Errorf("%s: round trip mismatch", key)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		assert.NoError(t, err)
	}
}

func TestClient_Compression_CloseIsIdempotent(t *testing.T) {
	client := newCompressionTestClient(t, newMemoryStore(), kv.CompressionParams{Enabled: true})

	require.NoError(t, client.Close())
	require.NoError(t, client.Close())
}
