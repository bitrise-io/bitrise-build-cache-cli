//go:build probe || integration

// Package fakebackend serves a local stand-in for bitrise-accelerate, for tests that need the
// real kv.Client rather than a mock.
package fakebackend

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"sync"
	"time"

	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/build/bazel/remote/execution/v2"
	kvstorage "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/kv_storage"
)

// Backend is a local stand-in for bitrise-accelerate, served over loopback
// for e2e-proxy-cache-macos.
//
// Using it keeps a PR gate off the shared backend: no credentials, no cross-DC
// variance, and no build artifacts written into a real workspace. Loopback is
// fast enough that its sensitivity was in doubt, so it was adopted only after
// confirming a throttled proxy still times out here and an unthrottled one does
// not — re-run that control before changing anything here. Numbers in
// docs/daemon-latency.md.
//
// Hits and misses are decided by hashing the key rather than randomly, so a run
// is reproducible: a miss costs the client seconds, and a random draw makes two
// otherwise identical runs do different amounts of work.
type Backend struct {
	kvstorage.UnimplementedKVStorageServer
	remoteexecution.UnimplementedCapabilitiesServer

	hitRate float64
	// Delay simulates a proxy that cannot keep up, which is what makes
	// operations pile up and the runtime grow threads. Without it a loopback
	// backend answers instantly and no concurrency ever builds.
	Delay time.Duration

	mu    sync.Mutex
	blobs map[string][]byte
}

func New(hitRate float64) *Backend {
	return &Backend{hitRate: hitRate, blobs: map[string][]byte{}}
}

// isHit is deterministic in the key, so every arm sees the same hit/miss
// sequence for the same workload.
func (f *Backend) isHit(key string) bool {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))

	return float64(h.Sum32()%100)/100.0 < f.hitRate
}

// Serve starts the backend on a loopback port and returns its grpc:// endpoint.
func (f *Backend) Serve() (endpoint string, stop func(), err error) {
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

func (f *Backend) GetCapabilities(_ context.Context, _ *remoteexecution.GetCapabilitiesRequest) (*remoteexecution.ServerCapabilities, error) {
	return &remoteexecution.ServerCapabilities{}, nil
}

func (f *Backend) Put(stream grpc.ClientStreamingServer[bytestream.WriteRequest, bytestream.WriteResponse]) error {
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

func (f *Backend) Get(req *bytestream.ReadRequest, stream grpc.ServerStreamingServer[bytestream.ReadResponse]) error {
	if f.Delay > 0 {
		time.Sleep(f.Delay)
	}

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

func (f *Backend) Delete(_ context.Context, req *bytestream.ReadRequest) (*kvstorage.DeleteResponse, error) {
	f.mu.Lock()
	delete(f.blobs, req.GetResourceName())
	f.mu.Unlock()

	return &kvstorage.DeleteResponse{}, nil
}

func (f *Backend) WriteStatus(_ context.Context, req *bytestream.QueryWriteStatusRequest) (*bytestream.QueryWriteStatusResponse, error) {
	f.mu.Lock()
	body, ok := f.blobs[req.GetResourceName()]
	f.mu.Unlock()

	return &bytestream.QueryWriteStatusResponse{
		CommittedSize: int64(len(body)),
		Complete:      ok,
	}, nil
}
