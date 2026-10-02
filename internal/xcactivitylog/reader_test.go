//go:build unit

package xcactivitylog_test

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

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
	// Multi-space / tab separators — matches live samples where SLF rendering wedges tabs between fields.
	path := writeGzipped(t, dir, "ws.xcactivitylog", []byte("prelude\n12\thits\t/\t24  cacheable  tasks  ( 50%)\ntail\n"))

	m, err := xcactivitylog.ReadCompilationCacheMetrics(path)
	require.NoError(t, err)

	// The space-inside-paren form (" 50%)") is permitted by the regex; the only strict parts are the keywords.
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
	// Mode 0 → os.Open returns EACCES; the reader must tag ReadError (not
	// FileMissing, not Unparsed) so callers can distinguish "couldn't look"
	// from "looked and found no match".
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
	// Fixture captured 2026-10-01 from CasProbe build (Xcode 26.x, logFormatVersion 11).
	// 75KB compressed, ~405KB decompressed, carries "65 hits / 65 cacheable tasks (100%)".
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

