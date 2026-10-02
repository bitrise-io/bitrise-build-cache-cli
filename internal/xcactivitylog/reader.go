// Package xcactivitylog extracts a narrow slice of information from Xcode's
// gzip-compressed `.xcactivitylog` files.
//
// The CompilationCacheMetrics section Xcode writes near the tail of a build log
// surfaces the compile-cache hit rate in the form:
//
//	note: 65 hits / 65 cacheable tasks (100%)
//
// (the "note:" prefix is from the diagnostic rendering; the same string appears
// again without the prefix as a section payload). Both occurrences carry the
// same numbers, so the first match wins.
//
// The reader streams the decompressed body with bufio.Scanner — the body is
// line-oriented SLF text and the metrics line sits near the tail, so a full
// ReadAll would waste memory on large projects. Scanner buffer is bumped past
// its 64KB default because individual SLF rows can exceed that.
package xcactivitylog

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"

	"github.com/bitrise-io/go-utils/v2/log"
)

// Outcome describes how ReadCompilationCacheMetrics terminated. Callers map
// the outcome into their own telemetry — the reader itself never logs.
type Outcome int

const (
	// OutcomeOK — matched the hit-rate line; counts are populated.
	OutcomeOK Outcome = iota
	// OutcomeFileMissing — path does not exist (ENOENT).
	OutcomeFileMissing
	// OutcomeEmpty — file exists but decompresses to zero bytes.
	OutcomeEmpty
	// OutcomeUnparsed — file has content but no CompilationCacheMetrics match.
	OutcomeUnparsed
)

// String renders Outcome for diagnostics; keep in sync with MetricsSource
// consts in internal/xcelerate/analytics so they can be grepped together.
func (o Outcome) String() string {
	switch o {
	case OutcomeOK:
		return "ok"
	case OutcomeFileMissing:
		return "file_missing"
	case OutcomeEmpty:
		return "empty"
	case OutcomeUnparsed:
		return "unparsed"
	default:
		return "unknown"
	}
}

// Pattern committed from a live sample (CasProbe build, Xcode 26.x, Apple's
// stock libToolchainCASPlugin, logFormatVersion 11). The surrounding "note:"
// prefix is dropped so a bare section payload also matches.
//
//nolint:gochecknoglobals // compiled once, read-only
var hitRateRegexp = regexp.MustCompile(`(\d+)\s+hits\s*/\s*(\d+)\s+cacheable\s+tasks\s*\(\s*(\d+)%\s*\)`)

// largeLogThresholdBytes bounds the Debug-log telemetry on pathological
// projects. The reader streams so there's no memory cliff, but recording the
// decompressed size when it crosses the line lets us revisit bounded reads
// in v2 if real builds routinely exceed it.
const largeLogThresholdBytes = 20 * 1024 * 1024

// Metrics is the structured return of ReadCompilationCacheMetrics so callers
// don't have to juggle five positional returns.
type Metrics struct {
	HitRate float32
	Hits    int
	Total   int
	Outcome Outcome
}

// ReadCompilationCacheMetrics opens an Xcode `.xcactivitylog` (gzip-compressed
// SLF text), scans for the CompilationCacheMetrics line, and returns the
// parsed hit rate (0.0-1.0 float, matching analytics.Invocation.HitRate),
// hit count, cacheable-task total, and outcome tag.
//
// On OutcomeFileMissing / OutcomeEmpty / OutcomeUnparsed the returned numbers
// are zero and err is nil — these are expected states the caller tags, not
// failures. A non-nil err indicates a filesystem / gzip read failure the
// caller should surface.
func ReadCompilationCacheMetrics(path string) (Metrics, error) {
	return readMetrics(path, nil)
}

// ReadCompilationCacheMetricsWithLogger is the variant used by callers that
// want the >20MB Debug telemetry. Nil logger disables it.
func ReadCompilationCacheMetricsWithLogger(path string, logger log.Logger) (Metrics, error) {
	return readMetrics(path, logger)
}

func readMetrics(path string, logger log.Logger) (Metrics, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Metrics{Outcome: OutcomeFileMissing}, nil
		}

		return Metrics{Outcome: OutcomeUnparsed}, fmt.Errorf("open xcactivitylog: %w", err)
	}
	defer f.Close()

	// A zero-length file reads EOF from the gzip header; anything else that
	// fails is a truncated / non-gzip body we tag as unparsed. Both cases are
	// outcome-only — the enricher still PUTs with MetricsSource set.
	stat, statErr := f.Stat()
	if statErr == nil && stat.Size() == 0 {
		return Metrics{Outcome: OutcomeEmpty}, nil
	}

	gz, err := gzip.NewReader(f)
	if err != nil {
		return Metrics{Outcome: OutcomeUnparsed}, nil //nolint:nilerr // bad body is an outcome, not a reader error
	}
	defer gz.Close()

	scanner := bufio.NewScanner(gz)
	// SLF rows can exceed the 64KB default when a single payload (file list,
	// signature) is long; bump the max token size.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var (
		bytesRead int64
		sawBytes  bool
	)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) > 0 {
			sawBytes = true
		}

		bytesRead += int64(len(line)) + 1 // approximate; +1 for the stripped delimiter

		if m := hitRateRegexp.FindSubmatch(line); m != nil {
			h, _ := strconv.Atoi(string(m[1]))
			tot, _ := strconv.Atoi(string(m[2]))

			var rate float32
			if tot > 0 {
				rate = float32(h) / float32(tot)
			}

			if logger != nil && bytesRead > largeLogThresholdBytes {
				logger.Debugf("xcactivitylog: large body (%d bytes decompressed) at %s — revisit bounded reads if common", bytesRead, path)
			}

			return Metrics{HitRate: rate, Hits: h, Total: tot, Outcome: OutcomeOK}, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return Metrics{Outcome: OutcomeUnparsed}, fmt.Errorf("scan xcactivitylog: %w", err)
	}

	if logger != nil && bytesRead > largeLogThresholdBytes {
		logger.Debugf("xcactivitylog: large body (%d bytes decompressed) at %s with no CompilationCacheMetrics match", bytesRead, path)
	}

	if !sawBytes {
		return Metrics{Outcome: OutcomeEmpty}, nil
	}

	return Metrics{Outcome: OutcomeUnparsed}, nil
}
