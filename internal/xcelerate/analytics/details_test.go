//go:build unit

package analytics_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

type capturedRequest struct {
	method  string
	path    string
	headers http.Header
	body    []byte
}

func newDetailsServer(t *testing.T, status int) (*httptest.Server, *[]capturedRequest) {
	t.Helper()

	var reqs []capturedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		reqs = append(reqs, capturedRequest{method: r.Method, path: r.URL.Path, headers: r.Header.Clone(), body: raw})
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	return srv, &reqs
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()

	zr, err := gzip.NewReader(bytes.NewReader(b))
	require.NoError(t, err)
	out, err := io.ReadAll(zr)
	require.NoError(t, err)

	return out
}

func TestClient_PutInvocationDetails_SendsGzippedVersionedPut(t *testing.T) {
	srv, reqs := newDetailsServer(t, http.StatusOK)
	client, err := analytics.NewClient(srv.URL, func() string { return "tok" }, log.NewLogger())
	require.NoError(t, err)

	body := []byte(`{"schemaVersion":1,"report":{"scheme":"App"}}`)
	require.NoError(t, client.PutInvocationDetails(context.Background(), "inv-1", body))

	require.Len(t, *reqs, 1)
	got := (*reqs)[0]
	assert.Equal(t, http.MethodPut, got.method)
	assert.Equal(t, "/invocations/inv-1/details", got.path)
	assert.Equal(t, "gzip", got.headers.Get("Content-Encoding"))
	assert.Equal(t, "application/json", got.headers.Get("Content-Type"))
	assert.Equal(t, "1", got.headers.Get("Version"))
	assert.Equal(t, "Bearer tok", got.headers.Get("Authorization"))
	assert.JSONEq(t, string(body), string(gunzip(t, got.body)))
}

func TestClient_PutInvocationDetails_Non2xxIsError(t *testing.T) {
	srv, _ := newDetailsServer(t, http.StatusNotFound)
	client, err := analytics.NewClient(srv.URL, nil, log.NewLogger())
	require.NoError(t, err)

	err = client.PutInvocationDetails(context.Background(), "inv-1", []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
}

func TestClient_PutInvocationDetails_HonorsCanceledContext(t *testing.T) {
	srv, reqs := newDetailsServer(t, http.StatusOK)
	client, err := analytics.NewClient(srv.URL, nil, log.NewLogger())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.Error(t, client.PutInvocationDetails(ctx, "inv-1", []byte(`{}`)))
	assert.Empty(t, *reqs)
}
