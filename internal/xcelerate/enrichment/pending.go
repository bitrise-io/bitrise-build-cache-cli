// Package enrichment re-PUTs Xcode invocations with LogStoreManifest metadata (F2).
package enrichment

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type PendingRecord struct {
	InvocationID string    `json:"invocation_id"`
	StartTime    time.Time `json:"start_time"`
	Duration     int64     `json:"duration_ms"`
	HitRate      float32   `json:"hit_rate"`
}

const EnrichmentPendingMaxAge = time.Hour

type Store struct {
	Path string
	Now  func() time.Time
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

func (s *Store) Append(rec PendingRecord) error {
	existing, err := s.Load()
	if err != nil {
		return err
	}

	cutoff := s.now().Add(-EnrichmentPendingMaxAge)
	kept := existing[:0]

	for _, r := range existing {
		if r.StartTime.Before(cutoff) {
			continue
		}

		kept = append(kept, r)
	}

	kept = append(kept, rec)

	return s.writeAtomic(kept)
}

func (s *Store) Load() ([]PendingRecord, error) {
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("open pending: %w", err)
	}
	defer f.Close()

	var out []PendingRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)

	for scanner.Scan() {
		var r PendingRecord
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			continue
		}

		out = append(out, r)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan pending: %w", err)
	}

	return out, nil
}

func (s *Store) Remove(invocationID string) error {
	existing, err := s.Load()
	if err != nil {
		return err
	}

	kept := existing[:0]

	for _, r := range existing {
		if r.InvocationID == invocationID {
			continue
		}

		kept = append(kept, r)
	}

	return s.writeAtomic(kept)
}

func (s *Store) writeAtomic(records []PendingRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("mkdir pending dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".pending-*.tmp")
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

			return fmt.Errorf("encode pending: %w", err)
		}
	}

	if err := w.Flush(); err != nil {
		tmp.Close()

		return fmt.Errorf("flush pending: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Rename(tmpPath, s.Path); err != nil {
		return fmt.Errorf("rename pending: %w", err)
	}

	return nil
}
