package enrichment

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HandledManifestMaxAge bounds how far back the Watcher will replay a UUID as new.
// Older records are pruned at startup so the file doesn't grow forever.
const HandledManifestMaxAge = 7 * 24 * time.Hour

// HandledManifest is one line in the NDJSON log.
type HandledManifest struct {
	UUID      string    `json:"uuid"`
	HandledAt time.Time `json:"handled_at"`
}

// HandledManifestStore persists the Watcher's seen-UUID set across proxy restarts.
type HandledManifestStore struct {
	Path string

	mu sync.Mutex
}

// Load reads every record from disk. Missing file returns nil, nil.
// Malformed lines are skipped.
func (s *HandledManifestStore) Load() ([]HandledManifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadLocked()
}

func (s *HandledManifestStore) loadLocked() ([]HandledManifest, error) {
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("open handled manifests: %w", err)
	}
	defer f.Close()

	var out []HandledManifest
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)

	for scanner.Scan() {
		var r HandledManifest
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			continue
		}

		out = append(out, r)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan handled manifests: %w", err)
	}

	return out, nil
}

// Append writes one record as a new NDJSON line.
func (s *HandledManifestStore) Append(rec HandledManifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("mkdir handled manifests dir: %w", err)
	}

	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open handled manifests append: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal handled manifest: %w", err)
	}

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write handled manifest: %w", err)
	}

	return nil
}

// PruneOlderThan drops records whose HandledAt is older than now-maxAge.
// No-op when nothing is stale (avoids an unnecessary rewrite).
func (s *HandledManifestStore) PruneOlderThan(now time.Time, maxAge time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadLocked()
	if err != nil {
		return err
	}

	if len(records) == 0 {
		return nil
	}

	cutoff := now.Add(-maxAge)
	kept := make([]HandledManifest, 0, len(records))
	pruned := 0

	for _, r := range records {
		if r.HandledAt.Before(cutoff) {
			pruned++

			continue
		}

		kept = append(kept, r)
	}

	if pruned == 0 {
		return nil
	}

	return s.writeAtomicLocked(kept)
}

// generic helper would need reflect / json.RawMessage indirection just to share ~40 lines.
//
//nolint:dupl // NDJSON writeAtomic is intentionally per-type — extracting to a
func (s *HandledManifestStore) writeAtomicLocked(records []HandledManifest) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("mkdir handled manifests dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".handled-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	defer func() {
		_ = os.Remove(tmpPath)
	}()

	w := bufio.NewWriter(tmp)
	enc := json.NewEncoder(w)

	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			tmp.Close()

			return fmt.Errorf("encode handled manifest: %w", err)
		}
	}

	if err := w.Flush(); err != nil {
		tmp.Close()

		return fmt.Errorf("flush handled manifests: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Rename(tmpPath, s.Path); err != nil {
		return fmt.Errorf("rename handled manifests: %w", err)
	}

	return nil
}
