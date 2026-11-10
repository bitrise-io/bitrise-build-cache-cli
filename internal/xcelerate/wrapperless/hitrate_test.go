//go:build unit

package wrapperless

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

// writeXCActivityLog writes a gzipped xcactivitylog carrying a
// CompilationCacheMetrics line the reader can parse.
func writeXCActivityLog(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "log.xcactivitylog")

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write([]byte(body))
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644)) //nolint:gosec // test file

	return path
}

func blobDownload(opCount, miss int64) *blobstats.Snapshot {
	return &blobstats.Snapshot{
		Download: blobstats.DirectionSnapshot{OpCount: opCount, MissCount: miss},
	}
}

func blobAndKVDownload(blobOp, blobMiss, kvOp, kvMiss int64) *blobstats.Snapshot {
	return &blobstats.Snapshot{
		Download: blobstats.DirectionSnapshot{OpCount: blobOp, MissCount: blobMiss},
		KV: &blobstats.ProtocolSnapshot{
			Download: blobstats.DirectionSnapshot{OpCount: kvOp, MissCount: kvMiss},
		},
	}
}

func TestComputeHitRate(t *testing.T) {
	dir := t.TempDir()
	logBody := "prelude\nnote: 3 hits / 4 cacheable tasks (75%)\ntail\n"
	logPath := writeXCActivityLog(t, dir, logBody)

	cases := []struct {
		name   string
		bucket Bucket
		want   float32
	}{
		{
			name:   "empty bucket yields zero",
			bucket: Bucket{},
			want:   0,
		},
		{
			name:   "blob-only: 8 hits / 10 ops",
			bucket: Bucket{BlobStats: blobDownload(10, 2)},
			want:   0.8,
		},
		{
			name:   "blob zero ops stays zero",
			bucket: Bucket{BlobStats: blobDownload(0, 0)},
			want:   0,
		},
		{
			name: "blob+KV: KV overrides blob",
			// Blob would be 0.8, KV is 0.5 and wins.
			bucket: Bucket{BlobStats: blobAndKVDownload(10, 2, 4, 2)},
			want:   0.5,
		},
		{
			name: "blob+KV with KV zero ops: KV skipped, blob stays",
			// KV OpCount == 0 so KV tier doesn't apply.
			bucket: Bucket{BlobStats: blobAndKVDownload(10, 5, 0, 0)},
			want:   0.5,
		},
		{
			name: "blob+log: log overrides blob (0.75 vs 0.8)",
			bucket: Bucket{
				BlobStats:         blobDownload(10, 2),
				XCActivityLogPath: logPath,
			},
			want: 0.75,
		},
		{
			name: "blob+KV+log: log overrides KV (0.75 vs 0.5)",
			bucket: Bucket{
				BlobStats:         blobAndKVDownload(10, 2, 4, 2),
				XCActivityLogPath: logPath,
			},
			want: 0.75,
		},
		{
			name: "log path set but file missing: tier skipped",
			bucket: Bucket{
				BlobStats:         blobDownload(10, 2),
				XCActivityLogPath: filepath.Join(dir, "nope.xcactivitylog"),
			},
			want: 0.8,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeHitRate(tc.bucket)
			assert.InDelta(t, tc.want, got, 0.001)
		})
	}
}
