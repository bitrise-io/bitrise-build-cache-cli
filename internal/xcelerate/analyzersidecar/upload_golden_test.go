//go:build unit

package analyzersidecar_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analyzersidecar"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

// Pins the v1 wire shape: renaming a buildanalyzer.Report JSON tag, or changing
// the envelope, must fail here rather than silently reach the backend.
func TestUploadingWriter_GoldenV1Body(t *testing.T) {
	var (
		bodies  [][]byte
		headers http.Header
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		bodies = append(bodies, raw)
		headers = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client, err := analytics.NewClient(srv.URL, nil, log.NewLogger())
	require.NoError(t, err)

	w := analyzersidecar.NewUploadingWriter(paths.FromHome(t.TempDir()), nil, client)
	require.NoError(t, w.Write(context.Background(), "inv-golden", readGoldenReport(t)))

	require.Len(t, bodies, 1)
	assert.Equal(t, "1", headers.Get("Version"))
	assert.Equal(t, "gzip", headers.Get("Content-Encoding"))

	zr, err := gzip.NewReader(bytes.NewReader(bodies[0]))
	require.NoError(t, err)
	got, err := io.ReadAll(zr)
	require.NoError(t, err)

	want, err := os.ReadFile("testdata/details_v1_body.json")
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}

func readGoldenReport(t *testing.T) buildanalyzer.Report {
	t.Helper()

	f, err := os.Open("testdata/report.json")
	require.NoError(t, err)
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()

	var rep buildanalyzer.Report
	require.NoError(t, dec.Decode(&rep), "testdata/report.json no longer matches buildanalyzer.Report's JSON tags")

	return rep
}
