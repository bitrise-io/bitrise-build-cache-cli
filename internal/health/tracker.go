// Package health records the timestamp of the last successful build cache call
// so that callers can determine how recently the backend was reachable.
package health

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	stateDirRelative = ".local/state/bitrise-build-cache"
	healthFileName   = "health.json"
	schemaVersion    = 1
)

type record struct {
	LastSuccessAt time.Time `json:"lastSuccessAt"`
	SchemaVersion int       `json:"schemaVersion"`
}

// Tracker persists the timestamp of the last successful build cache call.
// All writes are atomic (tmp-file + rename) so concurrent calls are safe.
type Tracker struct {
	path string
}

// NewTracker creates a Tracker that stores state under homeDir.
func NewTracker(homeDir string) *Tracker {
	return &Tracker{
		path: filepath.Join(homeDir, stateDirRelative, healthFileName),
	}
}

// RecordSuccess persists the current UTC time as the last successful call.
// Errors are silently discarded — health tracking is best-effort.
func (t *Tracker) RecordSuccess() {
	_ = t.write(time.Now().UTC())
}

// LastSuccess returns the time of the last recorded successful call.
// Returns (zero, false, nil) when no record exists yet.
func (t *Tracker) LastSuccess() (time.Time, bool, error) {
	data, err := os.ReadFile(t.path)
	if os.IsNotExist(err) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read health file: %w", err)
	}

	var r record
	if err := json.Unmarshal(data, &r); err != nil {
		return time.Time{}, false, fmt.Errorf("decode health file: %w", err)
	}

	return r.LastSuccessAt, true, nil
}

// ---------------------------------------------------------------------------
// Private
// ---------------------------------------------------------------------------

func (t *Tracker) write(ts time.Time) error {
	dir := filepath.Dir(t.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create health state dir: %w", err)
	}

	data, err := json.Marshal(record{LastSuccessAt: ts, SchemaVersion: schemaVersion})
	if err != nil {
		return fmt.Errorf("marshal health record: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "health.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)

		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, t.path); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}
