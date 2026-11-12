package wrapperless

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

// HandledManifestMaxAge bounds how far back the DDWatcher replays a
// uniqueIdentifier as new. Older records are pruned at load time so the
// ledger does not grow forever.
//
// Deliberately larger than seedStaleGrace: that constant is a within-startup
// wall-clock window (did a manifest seal just before this proxy started?),
// while this one is cross-restart retention of the persisted seen-set, where
// the worst case is a dev machine idle over a long weekend before the next
// build.
const HandledManifestMaxAge = 7 * 24 * time.Hour

// HandledManifest is one NDJSON record in the ledger.
type HandledManifest struct {
	UUID      string    `json:"uuid"`
	HandledAt time.Time `json:"handled_at"`
}

// HandledManifestStore persists the DDWatcher's seen-uniqueIdentifier set
// across proxy restarts. Mirrors the enricher's HandledManifestStore in shape
// but is scoped to the wrapperless flow's own ledger file so the two sets
// cannot collide.
//
// The ledger closes the dup-row race within seedStaleGrace: a proxy restart
// seconds after a build sealed its manifest would otherwise seed-walk the
// still-fresh file, mint a new InvocationID (anchor differs because ProxyPID
// differs), and produce a second BE row for the same build.
type HandledManifestStore struct {
	Path string

	mu sync.Mutex
}

// Load reads every record from disk. Missing file returns nil, nil.
// Malformed lines are skipped so a truncated tail from a crash cannot block
// the next proxy from starting.
func (s *HandledManifestStore) Load() ([]HandledManifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadLocked()
}

func (s *HandledManifestStore) loadLocked() ([]HandledManifest, error) {
	f, err := os.Open(s.Path) //nolint:gosec // app-owned state file
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("open handled manifests: %w", err)
	}
	defer f.Close() //nolint:errcheck

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

	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // app-owned state file
	if err != nil {
		return fmt.Errorf("open handled manifests append: %w", err)
	}
	defer f.Close() //nolint:errcheck

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal handled manifest: %w", err)
	}

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write handled manifest: %w", err)
	}

	return nil
}

// LoadFresh returns the UUIDs whose HandledAt is within maxAge of now.
// Stale records are dropped from memory.
func (s *HandledManifestStore) LoadFresh(now time.Time, maxAge time.Duration) ([]string, error) {
	records, err := s.Load()
	if err != nil {
		return nil, err
	}

	cutoff := now.Add(-maxAge)
	out := make([]string, 0, len(records))

	for _, r := range records {
		if r.HandledAt.Before(cutoff) {
			continue
		}

		out = append(out, r.UUID)
	}

	return out, nil
}

// Compact rewrites the on-disk file keeping only records whose HandledAt is
// within maxAge of now. Atomic via tmp + rename so a crash mid-write cannot
// produce a partial ledger. No-op when the file does not exist or when no
// records would be dropped.
func (s *HandledManifestStore) Compact(now time.Time, maxAge time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.loadFreshAndCompactLocked(now, maxAge)

	return err
}

// LoadFreshAndCompact returns the UUIDs whose HandledAt is within maxAge of
// now, and in the same pass rewrites the on-disk ledger if any stale records
// were dropped. Single read + write path replaces LoadFresh followed by
// Compact at hydrate.
func (s *HandledManifestStore) LoadFreshAndCompact(now time.Time, maxAge time.Duration) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadFreshAndCompactLocked(now, maxAge)
}

func (s *HandledManifestStore) loadFreshAndCompactLocked(now time.Time, maxAge time.Duration) ([]string, error) {
	records, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}

	cutoff := now.Add(-maxAge)
	kept := make([]HandledManifest, 0, len(records))
	uuids := make([]string, 0, len(records))
	for _, r := range records {
		if r.HandledAt.Before(cutoff) {
			continue
		}

		kept = append(kept, r)
		uuids = append(uuids, r.UUID)
	}

	if len(kept) == len(records) {
		return uuids, nil
	}

	if err := s.rewriteLocked(kept); err != nil {
		return uuids, err
	}

	return uuids, nil
}

func (s *HandledManifestStore) rewriteLocked(kept []HandledManifest) error {
	tmp := s.Path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // app-owned state file
	if err != nil {
		return fmt.Errorf("open handled manifests tmp: %w", err)
	}

	w := bufio.NewWriter(f)
	for _, r := range kept {
		line, err := json.Marshal(r)
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)

			return fmt.Errorf("marshal handled manifest: %w", err)
		}

		if _, err := w.Write(append(line, '\n')); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)

			return fmt.Errorf("write handled manifest: %w", err)
		}
	}

	if err := w.Flush(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)

		return fmt.Errorf("flush handled manifests: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("close handled manifests tmp: %w", err)
	}

	if err := os.Rename(tmp, s.Path); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("rename handled manifests: %w", err)
	}

	return nil
}
