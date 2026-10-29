package proxy_test

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

// Point at a DerivedData/CompilationCache.noindex dir (or any dir of *.leaf CAS objects) to
// benchmark real Xcode blobs; without it a synthetic corpus is used and ratios are not meaningful.
const compressionCorpusEnv = "BITRISE_COMPRESSION_BENCH_CORPUS"

const (
	maxBlobBytes      = 8 << 20
	maxBucketBytes    = 64 << 20
	maxBlobsPerBucket = 64
)

var compressionSizeBuckets = []struct { //nolint:gochecknoglobals
	name     string
	min, max int
}{
	{"lt1KiB", 0, 1 << 10},
	{"1-16KiB", 1 << 10, 16 << 10},
	{"16-256KiB", 16 << 10, 256 << 10},
	{"256KiB-4MiB", 256 << 10, 4 << 20},
	{"4-8MiB", 4 << 20, maxBlobBytes + 1},
}

var compressionLevels = []struct { //nolint:gochecknoglobals
	name  string
	level zstd.EncoderLevel
}{
	{"fastest", zstd.SpeedFastest},
	{"default", zstd.SpeedDefault},
	{"better", zstd.SpeedBetterCompression},
}

// Single-threaded coders: the proxy compresses many blobs concurrently, so per-core cost is what matters.
func BenchmarkZstdCompress(b *testing.B) {
	corpus := loadCompressionCorpus(b)

	for _, bucket := range corpus {
		for _, lvl := range compressionLevels {
			b.Run(fmt.Sprintf("%s/%s", bucket.name, lvl.name), func(b *testing.B) {
				enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(lvl.level), zstd.WithEncoderConcurrency(1))
				require.NoError(b, err)
				defer enc.Close()

				var rawBytes, compressedBytes int64
				for _, blob := range bucket.blobs {
					rawBytes += int64(len(blob))
					compressedBytes += int64(len(enc.EncodeAll(blob, nil)))
				}

				b.SetBytes(rawBytes)

				var dst []byte
				for b.Loop() {
					for _, blob := range bucket.blobs {
						dst = enc.EncodeAll(blob, dst[:0])
					}
				}

				// b.Loop resets metrics on its first call, so report afterwards.
				b.ReportMetric(float64(rawBytes)/float64(compressedBytes), "ratio")
				b.ReportMetric(float64(len(bucket.blobs)), "blobs")
			})
		}
	}
}

func BenchmarkZstdDecompress(b *testing.B) {
	corpus := loadCompressionCorpus(b)

	for _, bucket := range corpus {
		for _, lvl := range compressionLevels {
			b.Run(fmt.Sprintf("%s/%s", bucket.name, lvl.name), func(b *testing.B) {
				enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(lvl.level), zstd.WithEncoderConcurrency(1))
				require.NoError(b, err)
				defer enc.Close()
				dec, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
				require.NoError(b, err)
				defer dec.Close()

				var rawBytes int64
				compressed := make([][]byte, len(bucket.blobs))
				for i, blob := range bucket.blobs {
					rawBytes += int64(len(blob))
					compressed[i] = enc.EncodeAll(blob, nil)
				}

				b.SetBytes(rawBytes)

				var dst []byte
				for b.Loop() {
					for _, c := range compressed {
						var err error
						dst, err = dec.DecodeAll(c, dst[:0])
						require.NoError(b, err)
					}
				}
			})
		}
	}
}

// Ratio per blob size, to pick the compression threshold: below the cutoff the saved bytes don't pay for the CPU.
func BenchmarkZstdRatioBySize(b *testing.B) {
	corpus := loadCompressionCorpus(b)

	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
	require.NoError(b, err)
	defer enc.Close()

	for _, bucket := range corpus {
		b.Run(bucket.name, func(b *testing.B) {
			var rawBytes, compressedBytes, grew int64
			for _, blob := range bucket.blobs {
				c := len(enc.EncodeAll(blob, nil))
				rawBytes += int64(len(blob))
				compressedBytes += int64(c)
				if c >= len(blob) {
					grew++
				}
			}

			for b.Loop() {
			}

			b.ReportMetric(float64(rawBytes)/float64(compressedBytes), "ratio")
			b.ReportMetric(float64(grew)/float64(len(bucket.blobs))*100, "%not-smaller")
		})
	}
}

type compressionBucket struct {
	name  string
	blobs [][]byte
}

func loadCompressionCorpus(b *testing.B) []compressionBucket {
	b.Helper()

	var buckets []compressionBucket
	if dir := os.Getenv(compressionCorpusEnv); dir != "" {
		buckets = realCompressionCorpus(b, dir)
	} else {
		b.Logf("%s not set: using a synthetic corpus, ratios are not representative", compressionCorpusEnv)
		buckets = syntheticCompressionCorpus()
	}

	var nonEmpty []compressionBucket
	for _, bucket := range buckets {
		if len(bucket.blobs) > 0 {
			nonEmpty = append(nonEmpty, bucket)
		}
	}
	require.NotEmpty(b, nonEmpty, "no blobs found")

	return nonEmpty
}

type corpusFile struct {
	path string
	size int
}

func realCompressionCorpus(b *testing.B, dir string) []compressionBucket {
	b.Helper()

	var files []corpusFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".leaf") {
			return nil //nolint:nilerr
		}
		info, err := d.Info()
		if err != nil || info.Size() == 0 || info.Size() > maxBlobBytes {
			return nil //nolint:nilerr
		}
		files = append(files, corpusFile{path, int(info.Size())})

		return nil
	})
	require.NoError(b, err)

	// Sorted + evenly strided so the sample is deterministic across runs.
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	buckets := make([]compressionBucket, len(compressionSizeBuckets))
	for i, sb := range compressionSizeBuckets {
		buckets[i].name = sb.name

		var inBucket []corpusFile
		for _, f := range files {
			if f.size >= sb.min && f.size < sb.max {
				inBucket = append(inBucket, f)
			}
		}

		stride := max(1, len(inBucket)/maxBlobsPerBucket)
		total := 0
		for j := 0; j < len(inBucket) && len(buckets[i].blobs) < maxBlobsPerBucket && total < maxBucketBytes; j += stride {
			data, err := os.ReadFile(inBucket[j].path)
			require.NoError(b, err)
			buckets[i].blobs = append(buckets[i].blobs, data)
			total += len(data)
		}
	}

	return buckets
}

func syntheticCompressionCorpus() []compressionBucket {
	rng := rand.New(rand.NewSource(1)) //nolint:gosec

	// Repeated record-like structure with some random payload: compresses, but not like real objects.
	gen := func(size int) []byte {
		out := make([]byte, 0, size)
		for len(out) < size {
			out = append(out, fmt.Sprintf("record-%d:type=%d:flags=%x;", rng.Intn(500), rng.Intn(8), rng.Intn(1<<16))...)
			junk := make([]byte, rng.Intn(24))
			rng.Read(junk)
			out = append(out, junk...)
		}

		return out[:size]
	}

	buckets := make([]compressionBucket, len(compressionSizeBuckets))
	for i, sb := range compressionSizeBuckets {
		buckets[i].name = sb.name
		size := max(sb.min*2, 512)
		if sb.max < maxBlobBytes {
			size = (sb.min + sb.max) / 2
		}
		for range 16 {
			buckets[i].blobs = append(buckets[i].blobs, gen(size))
		}
	}

	return buckets
}
