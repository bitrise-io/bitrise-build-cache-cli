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

// OpCount is the successful-transfer count (hits), MissCount is the miss
// count. Total attempts = hits + misses.
func blobDownload(hits, miss int64) *blobstats.Snapshot {
	return &blobstats.Snapshot{
		Download: blobstats.DirectionSnapshot{OpCount: hits, MissCount: miss},
	}
}

func blobAndKVDownload(blobHits, blobMiss, kvHits, kvMiss int64) *blobstats.Snapshot {
	return &blobstats.Snapshot{
		Download: blobstats.DirectionSnapshot{OpCount: blobHits, MissCount: blobMiss},
		KV: &blobstats.ProtocolSnapshot{
			Download: blobstats.DirectionSnapshot{OpCount: kvHits, MissCount: kvMiss},
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
			name:   "blob-only: 8 hits + 2 misses = 0.8",
			bucket: Bucket{BlobStats: blobDownload(8, 2)},
			want:   0.8,
		},
		{
			name:   "blob zero total stays zero",
			bucket: Bucket{BlobStats: blobDownload(0, 0)},
			want:   0,
		},
		{
			name:   "zero hits, many misses stays 0",
			bucket: Bucket{BlobStats: blobDownload(0, 5)},
			want:   0,
		},
		{
			name: "blob+KV: KV overrides blob",
			// Blob is 8 hits + 2 misses = 0.8; KV is 2 hits + 2 misses = 0.5, wins.
			bucket: Bucket{BlobStats: blobAndKVDownload(8, 2, 2, 2)},
			want:   0.5,
		},
		{
			name: "blob+KV with KV zero total: KV skipped, blob stays",
			// KV OpCount + MissCount == 0 so KV tier doesn't apply.
			bucket: Bucket{BlobStats: blobAndKVDownload(5, 5, 0, 0)},
			want:   0.5,
		},
		{
			name: "blob+log: log overrides blob (0.75 vs 0.8)",
			bucket: Bucket{
				BlobStats:         blobDownload(8, 2),
				XCActivityLogPath: logPath,
			},
			want: 0.75,
		},
		{
			name: "blob+KV+log: log overrides KV (0.75 vs 0.5)",
			bucket: Bucket{
				BlobStats:         blobAndKVDownload(8, 2, 2, 2),
				XCActivityLogPath: logPath,
			},
			want: 0.75,
		},
		{
			name: "log path set but file missing: tier skipped",
			bucket: Bucket{
				BlobStats:         blobDownload(8, 2),
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
