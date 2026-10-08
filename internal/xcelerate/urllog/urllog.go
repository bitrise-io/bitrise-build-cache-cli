// Package urllog is the per-proxy NDJSON log of enriched-invocation IDs the
// enricher writes as orphan PUTs land, read + truncated by stop-proxy to print
// a Visit URL per orphan the proxy emitted.
package urllog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Record is one NDJSON line of the per-proxy URL log.
type Record struct {
	InvocationID string    `json:"invocation_id"`
	EmittedAt    time.Time `json:"emitted_at"`
}

// Writer appends enriched-invocation IDs to a per-proxy NDJSON file. Safe for
// concurrent appends from multiple goroutines inside the same process via its
// internal mutex; cross-process safety is left to the single-proxy invariant
// that owns the pid-stamped filename.
type Writer struct {
	Path string

	mu sync.Mutex
}

// AppendEmittedURL satisfies enrichment.EmittedURLSink.
func (w *Writer) AppendEmittedURL(invocationID string) error {
	return w.Append(invocationID)
}

// Append writes one Record. Opens with O_APPEND|O_CREATE so a crashed proxy
// never leaks a half-line, fsyncs before close so stop-proxy's read observes it.
func (w *Writer) Append(invocationID string) error {
	rec := Record{InvocationID: invocationID, EmittedAt: time.Now().UTC()}

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal urllog record: %w", err)
	}
	line = append(line, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(w.Path), 0o755); err != nil {
		return fmt.Errorf("mkdir urllog dir: %w", err)
	}

	f, err := os.OpenFile(w.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open urllog: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write urllog record: %w", err)
	}

	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync urllog: %w", err)
	}

	return nil
}

// Read returns all records in the file in append order. Missing file → empty
// slice, nil error.
func Read(path string) ([]Record, error) {
	b, err := os.ReadFile(path) //nolint:gosec // caller-controlled path under the user's own state dir
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read urllog: %w", err)
	}

	trimmed := strings.TrimRight(string(b), "\n")
	if trimmed == "" {
		return nil, nil
	}

	out := make([]Record, 0, strings.Count(trimmed, "\n")+1)

	for _, line := range strings.Split(trimmed, "\n") {
		if line == "" {
			continue
		}

		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}

		out = append(out, rec)
	}

	return out, nil
}

// Delete removes the file; missing file is a no-op.
func Delete(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove urllog: %w", err)
	}

	return nil
}
