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
// The reader decompresses the body in full and regex-matches the whole buffer.
// SLF payloads can hold multi-MB rows (file lists, signatures) that trip
// bufio.Scanner even with a bumped buffer; peak memory == decompressed size,
// bounded in practice by Xcode log sizes (MB range) and recorded via Debug
// when the body crosses largeLogThresholdBytes.
package xcactivitylog

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
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
	// OutcomeReadError — file is present but open/read failed (EACCES, EIO,
	// EMFILE, EISDIR, scanner failure). Distinct from Unparsed: content was
	// never seen, so "no match" cannot be concluded.
	OutcomeReadError
)

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
// are zero and err is nil — expected states the caller tags, not failures.
// OutcomeReadError pairs with a non-nil err (open/scan failure); the caller
// should log the err and tag the row accordingly.
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

		return Metrics{Outcome: OutcomeReadError}, fmt.Errorf("open xcactivitylog: %w", err)
	}
	defer f.Close()

	// A zero-length file reads EOF from the gzip header; anything else that
	// fails is a truncated / non-gzip body we tag as unparsed.
	stat, statErr := f.Stat()
	if statErr == nil && stat.Size() == 0 {
		return Metrics{Outcome: OutcomeEmpty}, nil
	}

	gz, err := gzip.NewReader(f)
	if err != nil {
		return Metrics{Outcome: OutcomeUnparsed}, nil //nolint:nilerr // bad body is an outcome, not a reader error
	}
	defer gz.Close()

	body, err := io.ReadAll(gz)
	if err != nil {
		return Metrics{Outcome: OutcomeReadError}, fmt.Errorf("read xcactivitylog: %w", err)
	}

	bytesRead := int64(len(body))

	if logger != nil && bytesRead > largeLogThresholdBytes {
		logger.Debugf("xcactivitylog: large body (%d bytes decompressed) at %s — revisit bounded reads if common", bytesRead, path)
	}

	if bytesRead == 0 {
		return Metrics{Outcome: OutcomeEmpty}, nil
	}

	if m := hitRateRegexp.FindSubmatch(body); m != nil {
		h, _ := strconv.Atoi(string(m[1]))
		tot, _ := strconv.Atoi(string(m[2]))

		var rate float32
		if tot > 0 {
			rate = float32(h) / float32(tot)
		}

		return Metrics{HitRate: rate, Hits: h, Total: tot, Outcome: OutcomeOK}, nil
	}

	return Metrics{Outcome: OutcomeUnparsed}, nil
}
