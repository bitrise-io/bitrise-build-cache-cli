package slf

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Outcome tags how a ReadCompilationCacheMetrics call terminated. Mirrors
// the shape the legacy regex-based `internal/xcactivitylog` reader exposed
// so consumers (doctor, enricher) do not have to reshape their switch
// statements.
type Outcome int

const (
	OutcomeOK Outcome = iota
	OutcomeFileMissing
	OutcomeEmpty
	OutcomeUnparsed
	OutcomeReadError
)

// Metrics is the façade shape the legacy regex reader produced. HitRate
// is Hits / Total; both numbers are derived from the SLF v11+ attachment
// payloads (BuildOperationMetrics) and reflect Apple's own view of the
// compilation cache rather than a diagnostic-text scrape.
type Metrics struct {
	HitRate float32
	Hits    int
	Total   int
	Outcome Outcome
}

// LargeLogThresholdBytes bounds how big a decompressed log the caller can
// expect before the WithLogger variant emits a Debug marker. Carried over
// from the legacy regex reader so the "large body" observability stays
// consistent after the retirement.
const LargeLogThresholdBytes = 20 * 1024 * 1024

// DebugLogger is the minimal shape ReadCompilationCacheMetricsWithLogger
// expects; satisfied by `log.Logger`.
type DebugLogger interface {
	Debugf(format string, args ...any)
}

// ReadCompilationCacheMetrics opens the gzip-compressed `.xcactivitylog` at
// path, decodes the SLF tree, and sums the per-section
// CompilationCacheMetrics attachments into one hit-rate pair. Preserves the
// legacy contract: FileMissing / Empty / Unparsed return nil err; ReadError
// returns the open / read error.
func ReadCompilationCacheMetrics(path string) (Metrics, error) {
	return readMetrics(path, nil)
}

// ReadCompilationCacheMetricsWithLogger adds the >20MB Debug telemetry the
// legacy regex reader shipped. Nil logger disables it.
func ReadCompilationCacheMetricsWithLogger(path string, logger DebugLogger) (Metrics, error) {
	return readMetrics(path, logger)
}

func readMetrics(path string, logger DebugLogger) (Metrics, error) {
	f, err := os.Open(path) //nolint:gosec // caller-provided path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Metrics{Outcome: OutcomeFileMissing}, nil
		}

		return Metrics{Outcome: OutcomeReadError}, fmt.Errorf("open xcactivitylog: %w", err)
	}
	defer f.Close()

	stat, statErr := f.Stat()
	if statErr == nil && stat.Size() == 0 {
		return Metrics{Outcome: OutcomeEmpty}, nil
	}

	root, _, err := Decode(f)
	if err != nil {
		return Metrics{Outcome: OutcomeUnparsed}, nil //nolint:nilerr // bad body is an outcome
	}

	if logger != nil && statErr == nil && stat.Size() > LargeLogThresholdBytes/4 {
		// Compressed size at 1/4 of the uncompressed threshold is a loose but
		// cheap proxy — we don't want to re-read for the exact decompressed size.
		logger.Debugf("xcactivitylog: large body (compressed %d bytes) at %s", stat.Size(), path)
	}

	hits, total := sumCacheCounts(root)
	if total == 0 {
		return Metrics{Outcome: OutcomeUnparsed}, nil
	}

	rate := float32(hits) / float32(total)

	return Metrics{HitRate: rate, Hits: hits, Total: total, Outcome: OutcomeOK}, nil
}

// sumCacheCounts walks every attachment-bearing section and sums the Swift +
// Clang hits + misses from the BuildOperationMetrics counters. Both legacy
// (Xcode 15.3-26.3 flat keys) and current (Xcode 26.4+ counter dicts) shapes
// have already been normalised into Attachment.BuildOp.Counters by the
// decoder.
func sumCacheCounts(root *BuildStep) (int, int) {
	var hits, total int

	var walk func(b *BuildStep)
	walk = func(b *BuildStep) {
		for _, a := range b.Attachments {
			if a.BuildOp == nil {
				continue
			}

			for k, v := range a.BuildOp.Counters {
				switch k {
				case "swiftCacheHits", "clangCacheHits":
					hits += int(v)
					total += int(v)
				case "swiftCacheMisses", "clangCacheMisses":
					total += int(v)
				}
			}
		}

		for _, s := range b.SubSteps {
			walk(s)
		}
	}

	walk(root)

	return hits, total
}
