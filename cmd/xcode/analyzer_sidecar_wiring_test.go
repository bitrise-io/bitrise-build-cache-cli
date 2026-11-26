//go:build unit

package xcode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analyzersidecar"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

func countingDetailsClient(t *testing.T) (*analytics.Client, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client, err := analytics.NewClient(srv.URL, nil, bundleTestLogger)
	require.NoError(t, err)

	return client, &hits
}

func sidecarFilesFor(t *testing.T, p paths.Paths, invocationID string) []string {
	t.Helper()

	matches, err := filepath.Glob(p.AnalyzerSidecarFile("*", invocationID))
	require.NoError(t, err)

	return matches
}

func TestNewAnalyzerSidecarWriter_uploadsOnlyWhenGateOnAndClientSet(t *testing.T) {
	cases := map[string]struct {
		uploadEnabled bool
		withClient    bool
		wantUploading bool
		wantRequests  int32
	}{
		"gate off":            {withClient: true},
		"gate on":             {uploadEnabled: true, withClient: true, wantUploading: true, wantRequests: 1},
		"gate on, nil client": {uploadEnabled: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client, hits := countingDetailsClient(t)
			if !tc.withClient {
				client = nil
			}
			p := paths.FromHome(t.TempDir())

			w := newAnalyzerSidecarWriter(p, bundleTestLogger, client, tc.uploadEnabled)
			require.NoError(t, w.Write(context.Background(), "inv-1", buildanalyzer.Report{}))

			_, uploading := w.(*analyzersidecar.UploadingWriter)
			assert.Equal(t, tc.wantUploading, uploading)
			assert.Equal(t, tc.wantRequests, hits.Load())
			assert.Len(t, sidecarFilesFor(t, p, "inv-1"), 1, "sidecar is written regardless of the gate")
		})
	}
}

func TestXcodebuildRunner_resolveAnalyzerSidecar_uploadsOnlyWhenGateOn(t *testing.T) {
	cases := map[string]struct {
		config bool
		env    string
		want   bool
	}{
		"off":    {},
		"config": {config: true, want: true},
		"env":    {env: "true", want: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(xcelerate.EnvDetailsUpload, tc.env)
			c := &XcodebuildRunner{
				Config: xcelerate.Config{DetailsUploadEnabled: tc.config},
				Logger: bundleTestLogger,
				Paths:  paths.FromHome(t.TempDir()),
			}

			_, uploading := c.resolveAnalyzerSidecar().(*analyzersidecar.UploadingWriter)

			assert.Equal(t, tc.want, uploading)
		})
	}
}
