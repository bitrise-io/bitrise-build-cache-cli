//go:build unit

package proxy_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy/mocks"
	llvmcas "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/cas"
	llvmkv "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
)

// newBufconnProxy spins up a proxy backed by kvClient on a bufconn listener and
// returns a client connection. Shared helper for the ctx-decouple suite.
func newBufconnProxy(t *testing.T, kvClient *mocks.ClientMock) *grpc.ClientConn {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = listener.Close() })

	resolver.SetDefaultScheme("passthrough")
	cc, err := grpc.NewClient("bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	go func() {
		p := proxy.NewProxy(kvClient, true, mockLogger, func(string) (log.Logger, error) {
			return mockLogger, nil
		}, nil)
		_ = p.Serve(listener)
	}()

	return cc
}

// uploadGate wires three channels around the mocked UploadStreamToBuildCache
// call: uploadStarted fires once the handler enters the upload, release unblocks
// it, and ctxErr carries the ctx.Err() the mock observed at return time. The
// channel hand-off avoids a shared variable the race detector would flag.
type uploadGate struct {
	started chan struct{}
	release chan struct{}
	ctxErr  chan error
}

func newUploadGate() *uploadGate {
	return &uploadGate{
		started: make(chan struct{}),
		release: make(chan struct{}),
		ctxErr:  make(chan error, 1),
	}
}

func (g *uploadGate) fn() func(context.Context, io.ReadSeeker, string, int64) error {
	return func(ctx context.Context, _ io.ReadSeeker, _ string, _ int64) error {
		close(g.started)
		<-g.release
		g.ctxErr <- ctx.Err()

		return nil
	}
}

// TestSave_ClientCtxCancelDoesNotAbortUpload confirms CAS Save's upload call
// runs on a detached ctx so a cancelled client stream doesn't abort the PUT.
// The mock reports its ctx.Err() post-cancel; the test asserts no error.
func TestSave_ClientCtxCancelDoesNotAbortUpload(t *testing.T) {
	gate := newUploadGate()
	kvClient := &mocks.ClientMock{UploadStreamToBuildCacheFunc: gate.fn()}

	cc := newBufconnProxy(t, kvClient)
	casClient := llvmcas.NewCASDBServiceClient(cc)

	clientCtx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := casClient.Save(clientCtx, &llvmcas.CASSaveRequest{
			Data: &llvmcas.CASBlob{
				Blob: &llvmcas.CASBytes{Contents: &llvmcas.CASBytes_Data{Data: []byte("payload")}},
			},
		})
		done <- err
	}()

	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("upload never started")
	}

	cancel()
	close(gate.release)

	select {
	case err := <-gate.ctxErr:
		assert.NoError(t, err, "upload ctx was cancelled with the client")
	case <-time.After(5 * time.Second):
		t.Fatal("mock never returned")
	}

	<-done // client stream may or may not surface the cancel — don't depend on it

	stats, err := session.NewSessionClient(cc).GetSessionStats(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.NotNil(t, stats.GetCacheBlobStats())
	require.NotNil(t, stats.GetCacheBlobStats().GetCas())
	assert.Equal(t, int64(1), stats.GetCacheBlobStats().GetCas().GetUpload().GetOpCount(),
		"upload op must count once client cancel decoupled from upload")
}

// TestPut_ClientCtxCancelDoesNotAbortUpload mirrors Save for CAS Put.
func TestPut_ClientCtxCancelDoesNotAbortUpload(t *testing.T) {
	gate := newUploadGate()
	kvClient := &mocks.ClientMock{UploadStreamToBuildCacheFunc: gate.fn()}

	cc := newBufconnProxy(t, kvClient)
	casClient := llvmcas.NewCASDBServiceClient(cc)

	clientCtx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := casClient.Put(clientCtx, &llvmcas.CASPutRequest{
			Data: &llvmcas.CASObject{
				Blob: &llvmcas.CASBytes{Contents: &llvmcas.CASBytes_Data{Data: []byte("payload")}},
			},
		})
		done <- err
	}()

	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("upload never started")
	}

	cancel()
	close(gate.release)

	select {
	case err := <-gate.ctxErr:
		assert.NoError(t, err, "upload ctx was cancelled with the client")
	case <-time.After(5 * time.Second):
		t.Fatal("mock never returned")
	}

	<-done

	stats, err := session.NewSessionClient(cc).GetSessionStats(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.NotNil(t, stats.GetCacheBlobStats())
	require.NotNil(t, stats.GetCacheBlobStats().GetCas())
	assert.Equal(t, int64(1), stats.GetCacheBlobStats().GetCas().GetUpload().GetOpCount())
}

// TestPutValue_ClientCtxCancelDoesNotAbortUpload asserts the same decouple on
// the KV side; the op count must land under cas_blob_stats.kv.upload.
func TestPutValue_ClientCtxCancelDoesNotAbortUpload(t *testing.T) {
	gate := newUploadGate()
	kvClient := &mocks.ClientMock{UploadStreamToBuildCacheFunc: gate.fn()}

	cc := newBufconnProxy(t, kvClient)
	kvCli := llvmkv.NewKeyValueDBClient(cc)

	clientCtx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := kvCli.PutValue(clientCtx, &llvmkv.PutValueRequest{
			Key:   []byte("some-kv-key"),
			Value: &llvmkv.Value{Entries: map[string][]byte{"k": []byte("v")}},
		})
		done <- err
	}()

	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("upload never started")
	}

	cancel()
	close(gate.release)

	select {
	case err := <-gate.ctxErr:
		assert.NoError(t, err, "upload ctx was cancelled with the client")
	case <-time.After(5 * time.Second):
		t.Fatal("mock never returned")
	}

	<-done

	stats, err := session.NewSessionClient(cc).GetSessionStats(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.NotNil(t, stats.GetCacheBlobStats())
	require.NotNil(t, stats.GetCacheBlobStats().GetKv())
	assert.Equal(t, int64(1), stats.GetCacheBlobStats().GetKv().GetUpload().GetOpCount())
}

// TestGet_ClientCtxCancelStillPropagates is the negative test: downloads stay
// client-ctx-bound. A cancelled client stream must cancel the download ctx so
// the proxy doesn't fetch a blob nobody will read.
func TestGet_ClientCtxCancelStillPropagates(t *testing.T) {
	downloadStarted := make(chan struct{})
	downloadCtxErr := make(chan error, 1)

	kvClient := &mocks.ClientMock{
		DownloadStreamFunc: func(ctx context.Context, _ io.Writer, _ string) error {
			close(downloadStarted)
			<-ctx.Done()
			downloadCtxErr <- ctx.Err()

			return ctx.Err()
		},
	}

	cc := newBufconnProxy(t, kvClient)
	casClient := llvmcas.NewCASDBServiceClient(cc)

	clientCtx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_, _ = casClient.Get(clientCtx, &llvmcas.CASGetRequest{
			CasId: &llvmcas.CASDataID{Id: []byte("cancel-me")},
		})
		close(done)
	}()

	select {
	case <-downloadStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("download never started")
	}

	cancel()

	select {
	case err := <-downloadCtxErr:
		assert.ErrorIs(t, err, context.Canceled, "download ctx must propagate client cancel")
	case <-time.After(5 * time.Second):
		t.Fatal("download ctx never cancelled")
	}

	<-done
}

// TestUpload_HangsPastDecoupledTimeout confirms the detach timeout actually
// bounds a wedged backend: the shrunk timeout trips, the handler returns an
// upload error, and OpCount stays zero because the transfer never succeeded.
func TestUpload_HangsPastDecoupledTimeout(t *testing.T) {
	restore := proxy.SetUploadDecoupledTimeoutForTest(50 * time.Millisecond)
	t.Cleanup(restore)

	block := make(chan struct{})
	t.Cleanup(func() { close(block) })

	kvClient := &mocks.ClientMock{
		UploadStreamToBuildCacheFunc: func(ctx context.Context, _ io.ReadSeeker, _ string, _ int64) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-block:
				return nil
			}
		},
	}

	cc := newBufconnProxy(t, kvClient)
	casClient := llvmcas.NewCASDBServiceClient(cc)

	resp, err := casClient.Save(context.Background(), &llvmcas.CASSaveRequest{
		Data: &llvmcas.CASBlob{
			Blob: &llvmcas.CASBytes{Contents: &llvmcas.CASBytes_Data{Data: []byte("payload")}},
		},
	})
	require.NoError(t, err, "unary RPC itself must not surface the ctx — Save returns the error inline")
	require.NotNil(t, resp.GetError(), "handler must surface the timeout as a Save error")

	stats, err := session.NewSessionClient(cc).GetSessionStats(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	if cas := stats.GetCacheBlobStats().GetCas(); cas != nil {
		assert.Zero(t, cas.GetUpload().GetOpCount(),
			"timed-out upload must not count as a successful op")
	}
}
