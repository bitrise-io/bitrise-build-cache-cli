//go:build unit

package proxy_test

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy/mocks"
	llvmcas "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/cas"
)

// macOS caps sun_path at ~104 bytes; t.TempDir() goes way past that.
var sockCounter atomic.Int64

func shortTempSocket(t *testing.T) string {
	t.Helper()

	n := sockCounter.Add(1)
	p := filepath.Join(os.TempDir(), fmt.Sprintf("bbc-%d-%d.sock", os.Getpid(), n))

	t.Cleanup(func() { _ = os.Remove(p) })

	return p
}

func TestSidecar_WritesOneFilePerConnection(t *testing.T) {
	dir := t.TempDir()
	sockPath := shortTempSocket(t)

	raw, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })

	pl := proxy.NewPeerListener(raw)

	payload := []byte("blob-payload")
	kvClient := &mocks.ClientMock{
		DownloadStreamFunc: func(_ context.Context, w io.Writer, _ string) error {
			return gob.NewEncoder(w).Encode(struct {
				Data       []byte
				References [][]byte
			}{Data: payload})
		},
	}

	p := proxy.NewProxyWithOptions(
		kvClient, false, mockLogger,
		func(string) (log.Logger, error) { return mockLogger, nil },
		nil,
		proxy.SidecarOptions{Dir: dir, Listener: pl},
	)

	go func() { _ = p.Serve(pl) }()
	t.Cleanup(p.GracefulStop)

	// Two concurrent connections, each firing one Get.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			client, err := grpc.NewClient(
				"unix://"+sockPath,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			require.NoError(t, err)
			defer client.Close()

			casClient := llvmcas.NewCASDBServiceClient(client)
			_, callErr := casClient.Get(context.Background(), &llvmcas.CASGetRequest{
				CasId: &llvmcas.CASDataID{Id: []byte("blob-id")},
			})
			require.NoError(t, callErr)
		}()
	}
	wg.Wait()

	// Give the server a moment to see each ConnEnd and flush the sidecar.
	assert.Eventually(t, func() bool {
		files, _ := os.ReadDir(dir)
		count := 0
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			count++
		}

		return count >= 2
	}, 3*time.Second, 50*time.Millisecond, "expected at least 2 sidecar JSON files")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var jsonFiles []os.DirEntry
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			jsonFiles = append(jsonFiles, e)
		}
	}
	require.GreaterOrEqual(t, len(jsonFiles), 2)

	for _, f := range jsonFiles {
		body, err := os.ReadFile(filepath.Join(dir, f.Name()))
		require.NoError(t, err)

		var sc proxy.SessionSidecar
		require.NoError(t, json.Unmarshal(body, &sc))

		assert.Equal(t, proxy.SidecarSchemaVersion, sc.SchemaVersion)
		assert.Equal(t, "proxy", sc.Source)
		assert.NotEmpty(t, sc.SidecarUUID)
		assert.Equal(t, os.Getpid(), sc.PeerPID, "peer PID equals this test process")
		assert.NotZero(t, sc.AcceptedAt)
		assert.NotZero(t, sc.ClosedAt)
		assert.Nil(t, sc.WrapperSession, "wrapper_session reserved for later PR")
		assert.Equal(t, int64(1), sc.Stats.Hits, "one Get call => one hit")
	}
}

func TestSidecar_GracefulStopOnNeverServedProxyDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	kvClient := &mocks.ClientMock{
		DownloadStreamFunc: func(context.Context, io.Writer, string) error { return kv.ErrCacheNotFound },
	}

	p := proxy.NewProxyWithOptions(
		kvClient, false, mockLogger,
		func(string) (log.Logger, error) { return mockLogger, nil },
		nil,
		proxy.SidecarOptions{Dir: dir, Listener: nil},
	)

	assert.NotPanics(t, p.GracefulStop)
}
