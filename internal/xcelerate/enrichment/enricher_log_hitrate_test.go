//go:build unit

package enrichment_test

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func writeLog(t *testing.T, dir, name string, body []byte) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(body)
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	require.NoError(t, os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644))
}

type enrichSetup struct {
	manifestDir  string
	manifestPath string
	logName      string
	store        *enrichment.Store
	captured     *analytics.Invocation
	enricher     *enrichment.Enricher
	group        enrichment.ManifestEntryGroup
	logBuf       *bytes.Buffer
}

// newEnrichSetup builds a canonical Enrich fixture (manifest dir + sibling log
// name + capturing mock + buffered logger). Shared across package tests that
// drive the full Enrich path.
func newEnrichSetup(t *testing.T) *enrichSetup {
	t.Helper()
	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))

	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}
	logName := "AAAA-BBBB.xcactivitylog"

	captured := &analytics.Invocation{}
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			*captured = inv

			return nil
		},
	}

	buf := &bytes.Buffer{}
	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		Logger: log.NewLogger(log.WithOutput(buf), log.WithDebugLog(true)),
	}

	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{{
		UUID:      "wrapperless",
		Signature: "Build MyScheme",
		FileName:  logName,
		Status:    "S",
		Start:     time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
		Stop:      time.Date(2026, 10, 1, 12, 0, 1, 0, time.UTC),
	}}}

	return &enrichSetup{
		manifestDir:  manifestDir,
		manifestPath: filepath.Join(manifestDir, "LogStoreManifest.plist"),
		logName:      logName,
		store:        store,
		captured:     captured,
		enricher:     e,
		group:        group,
		logBuf:       buf,
	}
}

// TestEnricher_LogHitRate_FlowsIntoInvocation is the sole enricher-level smoke
// test for the manifest-anchored hit-rate read. Outcome / log-line coverage
// lives in xcactivitylog.ReadHitRateFromManifest tests; this guards the wiring:
// the helper's result actually lands on the PUT invocation.
func TestEnricher_LogHitRate_FlowsIntoInvocation(t *testing.T) {
	s := newEnrichSetup(t)
	writeLog(t, s.manifestDir, s.logName, []byte("header\nnote: 7 hits / 10 cacheable tasks (70%)\n"))

	s.enricher.Enrich(s.manifestPath, s.group)

	assert.InDelta(t, float32(0.7), s.captured.HitRate, 0.001)
}

// Non-OK xcactivitylog outcomes mean the parse we'd attribute to the PUT is
// unknown — skip the PUT so the BE row does not carry a bogus zero-hit-rate.
func TestEnricher_SkipsPutInvocationWhenLogMissing(t *testing.T) {
	s := newEnrichSetup(t)
	// No log file staged — ReadHitRateFromManifest returns OutcomeFileMissing.

	mock := s.enricher.Client.(*InvocationPutterMock)
	s.enricher.Enrich(s.manifestPath, s.group)

	assert.Empty(t, mock.PutInvocationCalls(), "missing log must skip PUT")
	assert.Contains(t, s.logBuf.String(), "Enrichment PUT skipped")
}

func TestEnricher_SkipsPutInvocationWhenLogEmpty(t *testing.T) {
	s := newEnrichSetup(t)
	require.NoError(t, os.WriteFile(filepath.Join(s.manifestDir, s.logName), nil, 0o644))

	mock := s.enricher.Client.(*InvocationPutterMock)
	s.enricher.Enrich(s.manifestPath, s.group)

	assert.Empty(t, mock.PutInvocationCalls(), "empty log must skip PUT")
	assert.Contains(t, s.logBuf.String(), "Enrichment PUT skipped")
}

func TestEnricher_SkipsPutInvocationWhenLogUnparsed(t *testing.T) {
	s := newEnrichSetup(t)
	// Gzip body without the CompilationCacheMetrics line — OutcomeUnparsed.
	writeLog(t, s.manifestDir, s.logName, []byte("some unrelated xcactivitylog body\n"))

	mock := s.enricher.Client.(*InvocationPutterMock)
	s.enricher.Enrich(s.manifestPath, s.group)

	assert.Empty(t, mock.PutInvocationCalls(), "unparsed log must skip PUT")
	assert.Contains(t, s.logBuf.String(), "Enrichment PUT skipped")
}
