//go:build unit

package analyzersidecar_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analyzersidecar"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

type recordedPut struct {
	method  string
	path    string
	headers http.Header
	body    []byte
}

type detailsBackend struct {
	mu   sync.Mutex
	puts []recordedPut
}

func (b *detailsBackend) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return len(b.puts)
}

func newDetailsBackend(t *testing.T) (*detailsBackend, *analytics.Client) {
	t.Helper()

	backend := &detailsBackend{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		backend.mu.Lock()
		backend.puts = append(backend.puts, recordedPut{method: r.Method, path: r.URL.Path, headers: r.Header.Clone(), body: raw})
		backend.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client, err := analytics.NewClient(srv.URL, func() string { return "tok" }, log.NewLogger())
	require.NoError(t, err)

	return backend, client
}

var sidecarDay = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func sampleReport() buildanalyzer.Report {
	return buildanalyzer.Report{
		Build: buildanalyzer.BuildMeta{Title: "Build Foo"},
		Tasks: []buildanalyzer.Task{{Key: "Ld /Foo", Target: "Foo", Tool: "linker"}},
	}
}

func TestUploadingWriter_UploadsGzippedEnvelope(t *testing.T) {
	backend, client := newDetailsBackend(t)
	w := analyzersidecar.NewUploadingWriter(paths.FromHome(t.TempDir()), nil, client, true)

	require.NoError(t, w.Write(context.Background(), "inv-1", sampleReport()))

	require.Equal(t, 1, backend.count())
	put := backend.puts[0]
	assert.Equal(t, http.MethodPut, put.method)
	assert.Equal(t, "/invocations/inv-1/details", put.path)
	assert.Equal(t, "gzip", put.headers.Get("Content-Encoding"))
	assert.Equal(t, "1", put.headers.Get("Version"))
	assert.Equal(t, "Bearer tok", put.headers.Get("Authorization"))

	zr, err := gzip.NewReader(bytes.NewReader(put.body))
	require.NoError(t, err)
	raw, err := io.ReadAll(zr)
	require.NoError(t, err)

	var got struct {
		SchemaVersion int                  `json:"schemaVersion"`
		Report        buildanalyzer.Report `json:"report"`
	}
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, 1, got.SchemaVersion)
	assert.Equal(t, "Build Foo", got.Report.Build.Title)
	require.Len(t, got.Report.Tasks, 1)
	assert.Equal(t, "Ld /Foo", got.Report.Tasks[0].Key)
}

func TestUploadingWriter_GateOffSkipsUpload(t *testing.T) {
	backend, client := newDetailsBackend(t)
	p := paths.FromHome(t.TempDir())
	w := analyzersidecar.NewUploadingWriter(p, nil, client, analyzersidecar.DetailsUploadEnabled(map[string]string{}))
	w.Sidecar = &analyzersidecar.Writer{Paths: p, Clock: fixedClock(sidecarDay)}

	require.NoError(t, w.Write(context.Background(), "inv-1", sampleReport()))

	assert.Equal(t, 0, backend.count())
	assert.FileExists(t, p.AnalyzerSidecarFile("2026-11-25", "inv-1"))
}

func TestUploadingWriter_GateOnUploadsOnce(t *testing.T) {
	backend, client := newDetailsBackend(t)
	envs := map[string]string{analyzersidecar.EnvDetailsUpload: "1"}
	w := analyzersidecar.NewUploadingWriter(paths.FromHome(t.TempDir()), nil, client, analyzersidecar.DetailsUploadEnabled(envs))

	require.NoError(t, w.Write(context.Background(), "inv-1", sampleReport()))

	assert.Equal(t, 1, backend.count())
}

func TestDetailsUploadEnabled(t *testing.T) {
	for value, want := range map[string]bool{"": false, "0": false, "false": false, "nope": false, "1": true, "true": true} {
		assert.Equal(t, want, analyzersidecar.DetailsUploadEnabled(map[string]string{analyzersidecar.EnvDetailsUpload: value}), "value=%q", value)
	}
}

type fakeSidecar struct{ err error }

func (f *fakeSidecar) Write(context.Context, string, buildanalyzer.Report) error { return f.err }

type fakePutter struct {
	calls int
	err   error
}

func (f *fakePutter) PutInvocationDetails(context.Context, string, []byte) error {
	f.calls++

	return f.err
}

func TestUploadingWriter_SidecarErrorStillUploads(t *testing.T) {
	sidecarErr := errors.New("disk full")
	putter := &fakePutter{}
	w := &analyzersidecar.UploadingWriter{Sidecar: &fakeSidecar{err: sidecarErr}, Client: putter}

	err := w.Write(context.Background(), "inv-1", sampleReport())

	require.ErrorIs(t, err, sidecarErr)
	assert.Equal(t, 1, putter.calls)
}

func TestUploadingWriter_UploadErrorStillWritesSidecar(t *testing.T) {
	p := paths.FromHome(t.TempDir())
	uploadErr := errors.New("HTTP 404")
	w := &analyzersidecar.UploadingWriter{
		Sidecar: &analyzersidecar.Writer{Paths: p, Clock: fixedClock(sidecarDay)},
		Client:  &fakePutter{err: uploadErr},
	}

	err := w.Write(context.Background(), "inv-1", sampleReport())

	require.ErrorIs(t, err, uploadErr)
	body, readErr := os.ReadFile(p.AnalyzerSidecarFile("2026-11-25", "inv-1")) //nolint:gosec // test file
	require.NoError(t, readErr)
	assert.Contains(t, string(body), "Build Foo")
}

func TestUploadingWriter_BothErrorsJoined(t *testing.T) {
	sidecarErr := errors.New("disk full")
	uploadErr := errors.New("HTTP 500")
	w := &analyzersidecar.UploadingWriter{Sidecar: &fakeSidecar{err: sidecarErr}, Client: &fakePutter{err: uploadErr}}

	err := w.Write(context.Background(), "inv-1", sampleReport())

	require.ErrorIs(t, err, sidecarErr)
	require.ErrorIs(t, err, uploadErr)
}
