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

func writeRaw(t *testing.T, dir, name string, body []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), body, 0o644))
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
		Logger: log.NewLogger(log.WithOutput(buf)),
	}

	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{{
		UUID:      "orphan",
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

func TestEnricher_LogHitRate_ActivityLog(t *testing.T) {
	s := newEnrichSetup(t)
	writeLog(t, s.manifestDir, s.logName, []byte("header\nnote: 7 hits / 10 cacheable tasks (70%)\n"))

	s.enricher.Enrich(s.manifestPath, s.group)

	assert.InDelta(t, float32(0.7), s.captured.HitRate, 0.001)
}

func TestEnricher_LogHitRate_LogMissing_NoWarn(t *testing.T) {
	s := newEnrichSetup(t)
	// No log written — sibling absent is the common case; must not warn.

	s.enricher.Enrich(s.manifestPath, s.group)

	assert.Zero(t, s.captured.HitRate)
	assert.NotContains(t, s.logBuf.String(), "[WARN]")
}

func TestEnricher_LogHitRate_LogEmpty_NoWarn(t *testing.T) {
	s := newEnrichSetup(t)
	writeRaw(t, s.manifestDir, s.logName, nil)

	s.enricher.Enrich(s.manifestPath, s.group)

	assert.Zero(t, s.captured.HitRate)
	assert.NotContains(t, s.logBuf.String(), "[WARN]")
}

func TestEnricher_LogHitRate_LogUnparsed_WarnsForDrift(t *testing.T) {
	s := newEnrichSetup(t)
	writeLog(t, s.manifestDir, s.logName, []byte("random SLF noise with no CompilationCacheMetrics line\n"))

	s.enricher.Enrich(s.manifestPath, s.group)

	assert.Zero(t, s.captured.HitRate)
	// Unparsed signals Xcode format drift — must surface a local Warn.
	assert.Contains(t, s.logBuf.String(), "xcactivitylog unparsed")
}

func TestEnricher_LogHitRate_EmptyManifestPath_FastReturn(t *testing.T) {
	// "" manifestPath must still PUT with zero hit rate (test shims / legacy entry points).
	s := newEnrichSetup(t)

	s.enricher.Enrich("", s.group)

	assert.Zero(t, s.captured.HitRate)
	assert.NotEmpty(t, s.captured.InvocationID)
}

func TestEnricher_LogHitRate_ReadFails_WarnsAndSkips(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory-mode permission checks")
	}
	s := newEnrichSetup(t)
	writeLog(t, s.manifestDir, s.logName, []byte("note: 1 hits / 1 cacheable tasks (100%)\n"))
	require.NoError(t, os.Chmod(s.manifestDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(s.manifestDir, 0o755) })

	s.enricher.Enrich(s.manifestPath, s.group)

	assert.Zero(t, s.captured.HitRate)
	assert.Contains(t, s.logBuf.String(), "xcactivitylog read failed")
}

func TestEnricher_LogHitRate_MissingFileName_FastReturn(t *testing.T) {
	s := newEnrichSetup(t)
	// Malformed manifest: primary entry has no FileName.
	s.group = enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{{
		UUID:      "orphan",
		Signature: "Build MyScheme",
		Status:    "S",
		Start:     time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
		Stop:      time.Date(2026, 10, 1, 12, 0, 1, 0, time.UTC),
	}}}

	s.enricher.Enrich(s.manifestPath, s.group)

	assert.Zero(t, s.captured.HitRate)
	assert.NotEmpty(t, s.captured.InvocationID)
}
