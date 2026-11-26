// Package analyzersidecar persists a buildanalyzer.Report as a per-invocation
// JSON file co-located with the daily ndjson under InvocationsDir. The writer
// is strictly auxiliary: it never writes into the ndjson itself, so the local
// invocation log's byte-level cross-writer parity contract stays intact.
// UploadingWriter additionally uploads the written sidecar through an injected client.
package analyzersidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

const (
	dayLayout = "2006-01-02"
	tmpPrefix = ".tmp-"
)

// Writer serializes an analyzer Report under `<InvocationsDir>/analyzer-sidecars/<day>/<InvocationID>.analyzer.json`.
// Day is derived from Clock() in UTC to match the ndjson day convention.
type Writer struct {
	Paths  paths.Paths
	Clock  func() time.Time
	Logger log.Logger
}

// NewWriter returns a Writer rooted at p; Clock defaults to time.Now.
func NewWriter(p paths.Paths, logger log.Logger) *Writer {
	return &Writer{Paths: p, Logger: logger}
}

// Write encodes rep under the per-day sidecar dir using atomic tmp+rename so
// a crash mid-encode never leaves a half-formed file on disk. The caller
// controls lifetime via ctx — a canceled ctx skips the write entirely.
func (w *Writer) Write(ctx context.Context, invocationID string, rep buildanalyzer.Report) error {
	if w == nil {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("analyzer sidecar: %w", err)
	}

	if invocationID == "" {
		return fmt.Errorf("analyzer sidecar: invocationID is required")
	}

	day := w.now().UTC().Format(dayLayout)
	dir := w.Paths.AnalyzerSidecarDir(day)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("analyzer sidecar: mkdir %s: %w", dir, err)
	}

	final := w.Paths.AnalyzerSidecarFile(day, invocationID)

	tmp, err := os.CreateTemp(dir, tmpPrefix+invocationID+"-*")
	if err != nil {
		return fmt.Errorf("analyzer sidecar: create tmp under %s: %w", dir, err)
	}

	tmpPath := tmp.Name()
	// A failure past this point leaves the tmp file orphaned; the final rename
	// is skipped so no partial file ever lands at the stable name.
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")

	if err := enc.Encode(rep); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("analyzer sidecar: encode: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("analyzer sidecar: close tmp: %w", err)
	}

	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("analyzer sidecar: chmod tmp: %w", err)
	}

	if err := os.Rename(tmpPath, final); err != nil {
		return fmt.Errorf("analyzer sidecar: rename %s: %w", final, err)
	}

	success = true

	if w.Logger != nil {
		w.Logger.Debugf("Analyzer sidecar written: %s", final)
	}

	return nil
}

func (w *Writer) now() time.Time {
	if w.Clock != nil {
		return w.Clock()
	}

	return time.Now()
}
