// Package xcactivitylog extracts the compile-cache hit rate from Xcode's
// gzip-compressed `.xcactivitylog` files.
//
// Uses ReadAll + regex rather than a line scanner because SLF payloads carry
// multi-MB rows (file lists, signatures) that trip bufio.Scanner even with a
// bumped buffer. Peak memory == decompressed size; logged as Debug past
// largeLogThresholdBytes.
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

// Outcome tags how the read terminated. The reader never logs — callers map
// outcomes into their own telemetry.
type Outcome int

const (
	OutcomeOK Outcome = iota
	OutcomeFileMissing
	OutcomeEmpty
	OutcomeUnparsed
	// OutcomeReadError — open/read failed; distinct from Unparsed because the
	// content was never seen, so "no match" cannot be concluded.
	OutcomeReadError
)

// hitRateRegexp matches both the diagnostic-rendered ("note: ...") and bare
// section-payload forms of the CompilationCacheMetrics line.
//
//nolint:gochecknoglobals // compiled once, read-only
var hitRateRegexp = regexp.MustCompile(`(\d+)\s+hits\s*/\s*(\d+)\s+cacheable\s+tasks\s*\(\s*(\d+)%\s*\)`)

const largeLogThresholdBytes = 20 * 1024 * 1024

type Metrics struct {
	HitRate float32
	Hits    int
	Total   int
	Outcome Outcome
}

// ReadCompilationCacheMetrics returns zero numbers + nil err for the
// FileMissing / Empty / Unparsed outcomes (expected states the caller tags).
// ReadError pairs with a non-nil err.
func ReadCompilationCacheMetrics(path string) (Metrics, error) {
	return readMetrics(path, nil)
}

// ReadCompilationCacheMetricsWithLogger adds the >20MB Debug telemetry. Nil
// logger disables it.
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

	// Zero-length would surface as gzip EOF; distinguish it from a truncated body.
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
