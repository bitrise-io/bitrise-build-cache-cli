// Package enrichment re-PUTs Xcode invocations with LogStoreManifest metadata (F2).
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

type PendingRecord struct {
	InvocationID    string          `json:"invocation_id"`
	StartTime       time.Time       `json:"start_time"`
	Duration        int64           `json:"duration_ms"`
	HitRate         float32         `json:"hit_rate"`
	FirstAttempt    time.Time       `json:"first_attempt,omitempty"`
	LastAttempt     time.Time       `json:"last_attempt,omitempty"`
	Attempts        int             `json:"attempts,omitempty"`
	LastError       string          `json:"last_error,omitempty"`
	EnrichedPayload json.RawMessage `json:"enriched_payload,omitempty"`
}

type Store struct {
	Path string
	Now  func() time.Time

	mu sync.Mutex
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

func (s *Store) Append(rec PendingRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.loadLocked()
	if err != nil {
		return err
	}

	return s.writeAtomic(append(existing, rec))
}

func (s *Store) Load() ([]PendingRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadLocked()
}

// Mutate runs fn under the store lock: it receives the current record slice,
// returns the new slice, and the result is written atomically. Callers that
// need read-modify-write against a shared file must go through this seam;
// separate Load + Save calls race with concurrent Append/Sweep.
func (s *Store) Mutate(fn func([]PendingRecord) []PendingRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.loadLocked()
	if err != nil {
		return err
	}

	return s.writeAtomic(fn(existing))
}

// PruneOrphansOlderThan drops records with Attempts == 0 whose StartTime is
// older than maxAge. Retrier calls this at startup so a wrapper crash between
// slim emit (appends untouched record) and F2 enrichment doesn't leave the
// queue growing indefinitely. Records with Attempts > 0 are the Retrier's
// concern (aged out by FirstAttempt) and left alone.
func (s *Store) PruneOrphansOlderThan(now time.Time, maxAge time.Duration) error {
	return s.Mutate(func(existing []PendingRecord) []PendingRecord {
		cutoff := now.Add(-maxAge)
		kept := existing[:0]

		for _, r := range existing {
			if r.Attempts == 0 && !r.StartTime.IsZero() && r.StartTime.Before(cutoff) {
				continue
			}

			kept = append(kept, r)
		}

		return kept
	})
}

func (s *Store) loadLocked() ([]PendingRecord, error) {
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
	return s.Mutate(func(existing []PendingRecord) []PendingRecord {
		kept := existing[:0]
		for _, r := range existing {
			if r.InvocationID == invocationID {
				continue
			}

			kept = append(kept, r)
		}

		return kept
	})
}

func (s *Store) Save(records []PendingRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeAtomic(records)
}

//nolint:dupl // paired with HandledManifestStore.writeAtomicLocked; see comment there.
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
