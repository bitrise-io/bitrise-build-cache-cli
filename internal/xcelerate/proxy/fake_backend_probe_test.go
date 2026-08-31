//go:build probe

package proxy_test

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"

	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/build/bazel/remote/execution/v2"
	kvstorage "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/kv_storage"
)

// fakeBackend is a local stand-in for bitrise-accelerate, served over loopback
// for e2e-daemon-cache-macos.
//
// Using it keeps a PR gate off the shared backend: no credentials, no cross-DC
// variance, and no build artifacts written into a real workspace. It was adopted
// only after confirming it still catches the regression — with the proxy flipped
// to ProcessType Background it recorded 38 timed-out operations against 0 on
// Interactive. Loopback is fast enough that this was genuinely in doubt, so
// re-run that control before changing anything here.
//
// Hits and misses are decided by hashing the key rather than randomly, so a run
// is reproducible: a miss costs the client seconds, and a random draw makes two
// otherwise identical runs do different amounts of work.
type fakeBackend struct {
	kvstorage.UnimplementedKVStorageServer
	remoteexecution.UnimplementedCapabilitiesServer

	hitRate float64

	mu    sync.Mutex
	blobs map[string][]byte
}

func newFakeBackend(hitRate float64) *fakeBackend {
	return &fakeBackend{hitRate: hitRate, blobs: map[string][]byte{}}
}

// isHit is deterministic in the key, so every arm sees the same hit/miss
// sequence for the same workload.
func (f *fakeBackend) isHit(key string) bool {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))

	return float64(h.Sum32()%100)/100.0 < f.hitRate
}

// serve starts the backend on a loopback port and returns its grpc:// endpoint.
func (f *fakeBackend) serve() (endpoint string, stop func(), err error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("listen: %w", err)
	}

	server := grpc.NewServer()
	kvstorage.RegisterKVStorageServer(server, f)
	remoteexecution.RegisterCapabilitiesServer(server, f)

	go func() { _ = server.Serve(listener) }()

	return "grpc://" + listener.Addr().String(), server.Stop, nil
}

func (f *fakeBackend) GetCapabilities(_ context.Context, _ *remoteexecution.GetCapabilitiesRequest) (*remoteexecution.ServerCapabilities, error) {
	return &remoteexecution.ServerCapabilities{}, nil
}

func (f *fakeBackend) Put(stream grpc.ClientStreamingServer[bytestream.WriteRequest, bytestream.WriteResponse]) error {
	var (
		name  string
		total int64
		body  []byte
	)

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("recv: %w", err)
		}
		if req.GetResourceName() != "" {
			name = req.GetResourceName()
		}
		body = append(body, req.GetData()...)
		total += int64(len(req.GetData()))
		if req.GetFinishWrite() {
			break
		}
	}

	f.mu.Lock()
	f.blobs[name] = body
	f.mu.Unlock()

	return stream.SendAndClose(&bytestream.WriteResponse{CommittedSize: total})
}

func (f *fakeBackend) Get(req *bytestream.ReadRequest, stream grpc.ServerStreamingServer[bytestream.ReadResponse]) error {
	if !f.isHit(req.GetResourceName()) {
		return status.Error(codes.NotFound, "simulated miss")
	}

	f.mu.Lock()
	body, ok := f.blobs[req.GetResourceName()]
	f.mu.Unlock()

	if !ok {
		return status.Error(codes.NotFound, "not stored")
	}

	return stream.Send(&bytestream.ReadResponse{Data: body})
}

func (f *fakeBackend) Delete(_ context.Context, req *bytestream.ReadRequest) (*kvstorage.DeleteResponse, error) {
	f.mu.Lock()
	delete(f.blobs, req.GetResourceName())
	f.mu.Unlock()

	return &kvstorage.DeleteResponse{}, nil
}

func (f *fakeBackend) WriteStatus(_ context.Context, req *bytestream.QueryWriteStatusRequest) (*bytestream.QueryWriteStatusResponse, error) {
	f.mu.Lock()
	body, ok := f.blobs[req.GetResourceName()]
	f.mu.Unlock()

	return &bytestream.QueryWriteStatusResponse{
		CommittedSize: int64(len(body)),
		Complete:      ok,
	}, nil
}

// TestFakeBackendServe runs the fake backend until killed, writing its endpoint
// to FAKE_BACKEND_ENDPOINT_FILE. The runner starts this once and points the
// proxy at it, so every arm talks to the same local server.
func TestFakeBackendServe(t *testing.T) {
	if os.Getenv("FAKE_BACKEND_SERVE") != "1" {
		t.Skip("set FAKE_BACKEND_SERVE=1 to run the fake backend")
	}

	hitRate := 0.5
	if v := os.Getenv("FAKE_BACKEND_HIT_RATE"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			hitRate = parsed
		}
	}

	endpoint, stop, err := newFakeBackend(hitRate).serve()
	if err != nil {
		t.Fatalf("serve fake backend: %v", err)
	}
	defer stop()

	t.Logf("fake backend listening on %s (hit rate %.2f)", endpoint, hitRate)

	if out := os.Getenv("FAKE_BACKEND_ENDPOINT_FILE"); out != "" {
		if err := os.WriteFile(out, []byte(endpoint+"\n"), 0o600); err != nil {
			t.Fatalf("write endpoint file: %v", err)
		}
	}

	// Killed by the runner when the arms are done.
	select {}
}
