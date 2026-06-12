//go:build unit

package updater

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManualUpgrade_dryRunDownloadsButDoesntExecute(t *testing.T) {
	// Fake "installer" — just enough to look like a shell script. Body is a
	// `true` so a tester executing it later would see exit 0.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\ntrue\n"))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	path, err := ManualUpgrade(context.Background(), ManualOptions{
		Bindir:       t.TempDir(),
		Logger:       loggerWithBuffer(&buf),
		InstallerURL: srv.URL,
		HTTPClient:   srv.Client(),
		DryRun:       true,
	})
	require.NoError(t, err)
	require.FileExists(t, path)
	defer func() { _ = os.Remove(path) }()

	body, err := os.ReadFile(path) //nolint:gosec // test temp file
	require.NoError(t, err)
	assert.Contains(t, string(body), "true")
	assert.Contains(t, buf.String(), "Dry run")
}

func TestManualUpgrade_executesInstallerAgainstBindir(t *testing.T) {
	bindir := t.TempDir()

	// Installer body writes a marker file to bindir/marker so the test can
	// observe that the script was executed AND received the -b flag.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\nset -e\nbindir=\"\"\nwhile getopts \"b:\" opt; do case $opt in b) bindir=$OPTARG ;; esac; done\necho INSTALLED > \"$bindir/marker\"\n"))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	_, err := ManualUpgrade(context.Background(), ManualOptions{
		Bindir:       bindir,
		Logger:       loggerWithBuffer(&buf),
		InstallerURL: srv.URL,
		HTTPClient:   srv.Client(),
	})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Upgrade complete")

	body, err := os.ReadFile(bindir + "/marker") //nolint:gosec // test bindir
	require.NoError(t, err)
	assert.Equal(t, "INSTALLED\n", string(body))
}

func TestManualUpgrade_requiresBindir(t *testing.T) {
	_, err := ManualUpgrade(context.Background(), ManualOptions{})
	require.Error(t, err)
}

func TestManualUpgrade_propagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := ManualUpgrade(context.Background(), ManualOptions{
		Bindir:       t.TempDir(),
		Logger:       loggerWithBuffer(&bytes.Buffer{}),
		InstallerURL: srv.URL,
		HTTPClient:   srv.Client(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestManualUpgrade_failingInstallerSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\nexit 17\n"))
	}))
	defer srv.Close()

	_, err := ManualUpgrade(context.Background(), ManualOptions{
		Bindir:       t.TempDir(),
		Logger:       loggerWithBuffer(&bytes.Buffer{}),
		InstallerURL: srv.URL,
		HTTPClient:   srv.Client(),
	})
	require.Error(t, err)
}

func TestManualUpgrade_rejectsOversizeInstaller(t *testing.T) {
	// Hostile origin streams 2 MiB. Download must abort instead of writing
	// the whole thing into os.TempDir.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("X"), 2*MaxInstallerBytes))
	}))
	defer srv.Close()

	_, err := ManualUpgrade(context.Background(), ManualOptions{
		Bindir:       t.TempDir(),
		Logger:       loggerWithBuffer(&bytes.Buffer{}),
		InstallerURL: srv.URL,
		HTTPClient:   srv.Client(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestManualUpgrade_rejectsNon2xxStatusIncludingRedirect(t *testing.T) {
	// Server emits a 301 with HTML body. The strict <200||>=300 check keeps
	// us from executing an HTML page as a shell script when an upstream
	// network appliance returns 3xx and the client isn't configured to
	// follow it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMovedPermanently)
		_, _ = w.Write([]byte("<html>moved</html>"))
	}))
	defer srv.Close()

	noRedirect := &http.Client{
		Transport: srv.Client().Transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	_, err := ManualUpgrade(context.Background(), ManualOptions{
		Bindir:       t.TempDir(),
		Logger:       loggerWithBuffer(&bytes.Buffer{}),
		InstallerURL: srv.URL,
		HTTPClient:   noRedirect,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "301")
}
