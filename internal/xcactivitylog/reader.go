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
	"path/filepath"
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

// HitRateRegexp matches the CompilationCacheMetrics line in both its stdout
// diagnostic form (`note: N hits / M cacheable tasks`) and its bare SLF
// payload form inside an .xcactivitylog; the trailing `(NN%)` suffix only
// appears in the SLF form, so it is optional.
//
//nolint:gochecknoglobals // compiled once, read-only
var HitRateRegexp = regexp.MustCompile(`(\d+)\s+hits?\s*/\s*(\d+)\s+cacheable\s+tasks(?:\s*\(\s*\d+%\s*\))?`)

// buildDescriptionHashRegexp captures the 32-hex build-description hash Xcode
// writes into the log body as a plain ASCII substring.
//
//nolint:gochecknoglobals // compiled once, read-only
var buildDescriptionHashRegexp = regexp.MustCompile(`Build description path: .*/XCBuildData/([0-9a-f]{32})\.xcbuilddata`)

const largeLogThresholdBytes = 20 * 1024 * 1024

type Metrics struct {
	HitRate float32
	Hits    int
	Total   int
	Outcome Outcome
	// BuildDescriptionHash is the 32-hex `XCBuildData/<hash>.xcbuilddata`
	// identifier Xcode prints in the log body. Best-effort; absence does NOT
	// demote OutcomeOK.
	BuildDescriptionHash string
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

	var hash string
	if m := buildDescriptionHashRegexp.FindSubmatch(body); m != nil {
		hash = string(m[1])
	}

	if m := HitRateRegexp.FindSubmatch(body); m != nil {
		h, _ := strconv.Atoi(string(m[1]))
		tot, _ := strconv.Atoi(string(m[2]))

		var rate float32
		if tot > 0 {
			rate = float32(h) / float32(tot)
		}

		return Metrics{HitRate: rate, Hits: h, Total: tot, Outcome: OutcomeOK, BuildDescriptionHash: hash}, nil
	}

	return Metrics{Outcome: OutcomeUnparsed, BuildDescriptionHash: hash}, nil
}

// ReadHitRateFromManifest resolves the sibling log path from manifestPath +
// logFileName, reads CompilationCacheMetrics, and logs non-OK outcomes via
// logger (debug for missing/empty, warn for unparsed with size hint, warn for
// read errors). Returns 0 rate + empty hash on any non-OK outcome. Empty
// inputs short-circuit as OutcomeFileMissing without reading.
func ReadHitRateFromManifest(manifestPath, logFileName string, logger log.Logger) (float32, Outcome, string) {
	m := ReadMetricsFromManifest(manifestPath, logFileName, logger)
	if m.Outcome == OutcomeOK {
		return m.HitRate, OutcomeOK, m.BuildDescriptionHash
	}

	return 0, m.Outcome, ""
}

// ReadMetricsFromManifest is the raw-counters sibling of ReadHitRateFromManifest.
// Multi-action clusters sum Hits/Total across every parseable entry's log, so
// callers need the integer counters to compute a weighted aggregate hit rate
// — not just the per-log rate.
func ReadMetricsFromManifest(manifestPath, logFileName string, logger log.Logger) Metrics {
	if manifestPath == "" || logFileName == "" {
		return Metrics{Outcome: OutcomeFileMissing}
	}

	logPath := filepath.Join(filepath.Dir(manifestPath), logFileName)

	metrics, err := ReadCompilationCacheMetricsWithLogger(logPath, logger)
	if err != nil && logger != nil {
		logger.Warnf("xcactivitylog read failed for %s: %s", logPath, err)
	}

	if logger != nil {
		switch metrics.Outcome {
		case OutcomeOK, OutcomeReadError:
		case OutcomeFileMissing:
			logger.Debugf("xcactivitylog missing at %s", logPath)
		case OutcomeEmpty:
			logger.Debugf("xcactivitylog empty at %s", logPath)
		case OutcomeUnparsed:
			size := int64(-1)
			if st, statErr := os.Stat(logPath); statErr == nil {
				size = st.Size()
			}
			logger.Warnf("xcactivitylog unparsed at %s (size=%d): no CompilationCacheMetrics match", logPath, size)
		}
	}

	return metrics
}
