//go:build unit

package xcactivitylog_test

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
)

func writeGzipped(t *testing.T, dir, name string, body []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(body)
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))

	return path
}

func TestReadCompilationCacheMetrics_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeGzipped(t, dir, "ok.xcactivitylog", []byte("header noise\nnote: 65 hits / 65 cacheable tasks (100%)\nmore\n"))

	m, err := xcactivitylog.ReadCompilationCacheMetrics(path)
	require.NoError(t, err)
	assert.Equal(t, xcactivitylog.OutcomeOK, m.Outcome)
	assert.Equal(t, 65, m.Hits)
	assert.Equal(t, 65, m.Total)
	assert.InDelta(t, float32(1.0), m.HitRate, 0.001)
}

func TestReadCompilationCacheMetrics_PartialHits(t *testing.T) {
	dir := t.TempDir()
	path := writeGzipped(t, dir, "partial.xcactivitylog", []byte("note: 7 hits / 10 cacheable tasks (70%)"))

	m, err := xcactivitylog.ReadCompilationCacheMetrics(path)
	require.NoError(t, err)
	assert.Equal(t, xcactivitylog.OutcomeOK, m.Outcome)
	assert.Equal(t, 7, m.Hits)
	assert.Equal(t, 10, m.Total)
	assert.InDelta(t, float32(0.7), m.HitRate, 0.001)
}

func TestReadCompilationCacheMetrics_WhitespaceVariations(t *testing.T) {
	dir := t.TempDir()
	// Multi-space / tab separators seen in live SLF samples.
	path := writeGzipped(t, dir, "ws.xcactivitylog", []byte("prelude\n12\thits\t/\t24  cacheable  tasks  ( 50%)\ntail\n"))

	m, err := xcactivitylog.ReadCompilationCacheMetrics(path)
	require.NoError(t, err)
	assert.Equal(t, xcactivitylog.OutcomeOK, m.Outcome)
	assert.Equal(t, 12, m.Hits)
	assert.Equal(t, 24, m.Total)
	assert.InDelta(t, float32(0.5), m.HitRate, 0.001)
}

func TestReadCompilationCacheMetrics_FileMissing(t *testing.T) {
	m, err := xcactivitylog.ReadCompilationCacheMetrics(filepath.Join(t.TempDir(), "nope.xcactivitylog"))
	require.NoError(t, err, "ENOENT must not surface as err — caller tags via outcome")
	assert.Equal(t, xcactivitylog.OutcomeFileMissing, m.Outcome)
	assert.Zero(t, m.HitRate)
	assert.Zero(t, m.Hits)
	assert.Zero(t, m.Total)
}

func TestReadCompilationCacheMetrics_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.xcactivitylog")
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	m, err := xcactivitylog.ReadCompilationCacheMetrics(path)
	require.NoError(t, err)
	assert.Equal(t, xcactivitylog.OutcomeEmpty, m.Outcome)
	assert.Zero(t, m.HitRate)
	assert.Zero(t, m.Hits)
	assert.Zero(t, m.Total)
}

func TestReadCompilationCacheMetrics_EmptyGzipBody(t *testing.T) {
	dir := t.TempDir()
	path := writeGzipped(t, dir, "empty-body.xcactivitylog", nil)

	m, err := xcactivitylog.ReadCompilationCacheMetrics(path)
	require.NoError(t, err)
	assert.Equal(t, xcactivitylog.OutcomeEmpty, m.Outcome)
	assert.Zero(t, m.HitRate)
	assert.Zero(t, m.Hits)
	assert.Zero(t, m.Total)
}

func TestReadCompilationCacheMetrics_NoMatch(t *testing.T) {
	dir := t.TempDir()
	path := writeGzipped(t, dir, "nomatch.xcactivitylog", []byte("Build description, lots of SLF noise, no metrics line here.\n"))

	m, err := xcactivitylog.ReadCompilationCacheMetrics(path)
	require.NoError(t, err)
	assert.Equal(t, xcactivitylog.OutcomeUnparsed, m.Outcome)
	assert.Zero(t, m.HitRate)
	assert.Zero(t, m.Hits)
	assert.Zero(t, m.Total)
}

func TestReadCompilationCacheMetrics_Unreadable_ReadError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permission checks")
	}
	// EACCES must tag ReadError (not FileMissing/Unparsed) so callers can tell
	// "couldn't look" from "looked and found no match".
	dir := t.TempDir()
	path := filepath.Join(dir, "noperm.xcactivitylog")
	require.NoError(t, os.WriteFile(path, []byte("payload"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	m, err := xcactivitylog.ReadCompilationCacheMetrics(path)
	require.Error(t, err)
	assert.Equal(t, xcactivitylog.OutcomeReadError, m.Outcome)
	assert.Zero(t, m.HitRate)
}

func TestReadCompilationCacheMetrics_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trunc.xcactivitylog")
	// First 3 bytes of a valid gzip header, missing the rest.
	require.NoError(t, os.WriteFile(path, []byte{0x1f, 0x8b, 0x08}, 0o644))

	m, err := xcactivitylog.ReadCompilationCacheMetrics(path)
	require.NoError(t, err, "truncated gzip must tag as unparsed, not error")
	assert.Equal(t, xcactivitylog.OutcomeUnparsed, m.Outcome)
}

func TestReadCompilationCacheMetrics_RealSample(t *testing.T) {
	// Fixture: CasProbe build (Xcode 26.x, logFormatVersion 11), ~405KB decompressed.
	path := filepath.Join("testdata", "sample.xcactivitylog")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("testdata sample not committed in this environment")
	}

	m, err := xcactivitylog.ReadCompilationCacheMetrics(path)
	require.NoError(t, err)
	assert.Equal(t, xcactivitylog.OutcomeOK, m.Outcome)
	assert.Equal(t, 65, m.Hits)
	assert.Equal(t, 65, m.Total)
	assert.InDelta(t, float32(1.0), m.HitRate, 0.001)
}

// Private — ReadHitRateFromManifest tests

func newManifestFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))

	return manifestDir, filepath.Join(manifestDir, "LogStoreManifest.plist")
}

func TestReadHitRateFromManifest_OK(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := log.NewLogger(log.WithOutput(buf))

	manifestDir, manifestPath := newManifestFixture(t)
	writeGzipped(t, manifestDir, "AAAA.xcactivitylog", []byte("note: 7 hits / 10 cacheable tasks (70%)"))

	rate, outcome := xcactivitylog.ReadHitRateFromManifest(manifestPath, "AAAA.xcactivitylog", logger)
	assert.Equal(t, xcactivitylog.OutcomeOK, outcome)
	assert.InDelta(t, float32(0.7), rate, 0.001)
	assert.NotContains(t, buf.String(), "[WARN]")
}

func TestReadHitRateFromManifest_FileMissing_Debug(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := log.NewLogger(log.WithOutput(buf))

	_, manifestPath := newManifestFixture(t)
	// No sibling log written.

	rate, outcome := xcactivitylog.ReadHitRateFromManifest(manifestPath, "AAAA.xcactivitylog", logger)
	assert.Equal(t, xcactivitylog.OutcomeFileMissing, outcome)
	assert.Zero(t, rate)
	assert.NotContains(t, buf.String(), "[WARN]")
}

func TestReadHitRateFromManifest_Empty_Debug(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := log.NewLogger(log.WithOutput(buf))

	manifestDir, manifestPath := newManifestFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, "AAAA.xcactivitylog"), nil, 0o644))

	rate, outcome := xcactivitylog.ReadHitRateFromManifest(manifestPath, "AAAA.xcactivitylog", logger)
	assert.Equal(t, xcactivitylog.OutcomeEmpty, outcome)
	assert.Zero(t, rate)
	assert.NotContains(t, buf.String(), "[WARN]")
}

func TestReadHitRateFromManifest_Unparsed_Warn(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := log.NewLogger(log.WithOutput(buf))

	manifestDir, manifestPath := newManifestFixture(t)
	writeGzipped(t, manifestDir, "AAAA.xcactivitylog", []byte("random SLF noise no metrics line here\n"))

	rate, outcome := xcactivitylog.ReadHitRateFromManifest(manifestPath, "AAAA.xcactivitylog", logger)
	assert.Equal(t, xcactivitylog.OutcomeUnparsed, outcome)
	assert.Zero(t, rate)
	// Unparsed signals Xcode format drift — must surface as warn with size hint.
	assert.Contains(t, buf.String(), "xcactivitylog unparsed")
	assert.Contains(t, buf.String(), "size=")
}

func TestReadHitRateFromManifest_EmptyManifestPath_FastReturn(t *testing.T) {
	rate, outcome := xcactivitylog.ReadHitRateFromManifest("", "AAAA.xcactivitylog", nil)
	assert.Equal(t, xcactivitylog.OutcomeFileMissing, outcome)
	assert.Zero(t, rate)
}

func TestReadHitRateFromManifest_EmptyLogFileName_FastReturn(t *testing.T) {
	rate, outcome := xcactivitylog.ReadHitRateFromManifest("/tmp/LogStoreManifest.plist", "", nil)
	assert.Equal(t, xcactivitylog.OutcomeFileMissing, outcome)
	assert.Zero(t, rate)
}

func TestReadHitRateFromManifest_NilLogger_NoPanic(t *testing.T) {
	manifestDir, manifestPath := newManifestFixture(t)
	writeGzipped(t, manifestDir, "AAAA.xcactivitylog", []byte("note: 3 hits / 4 cacheable tasks (75%)"))

	rate, outcome := xcactivitylog.ReadHitRateFromManifest(manifestPath, "AAAA.xcactivitylog", nil)
	assert.Equal(t, xcactivitylog.OutcomeOK, outcome)
	assert.InDelta(t, float32(0.75), rate, 0.001)
}

func TestReadHitRateFromManifest_ReadFailure_Warn(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory-mode permission checks")
	}
	buf := &bytes.Buffer{}
	logger := log.NewLogger(log.WithOutput(buf))

	manifestDir, manifestPath := newManifestFixture(t)
	writeGzipped(t, manifestDir, "AAAA.xcactivitylog", []byte("note: 1 hits / 1 cacheable tasks (100%)\n"))
	require.NoError(t, os.Chmod(manifestDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(manifestDir, 0o755) })

	rate, outcome := xcactivitylog.ReadHitRateFromManifest(manifestPath, "AAAA.xcactivitylog", logger)
	assert.Equal(t, xcactivitylog.OutcomeReadError, outcome)
	assert.Zero(t, rate)
	assert.Contains(t, buf.String(), "xcactivitylog read failed")
}
