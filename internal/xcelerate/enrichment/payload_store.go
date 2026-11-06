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

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

// PayloadStatus tracks where a PayloadRecord sits in the single-ID merge-and-
// re-PUT lifecycle: Open while sidecars still accumulate, PendingTerminal once
// the terminal plist or crash-fallback fires, Emitted once the BE PUT lands.
type PayloadStatus string

const (
	PayloadStatusOpen            PayloadStatus = "open"
	PayloadStatusPendingTerminal PayloadStatus = "pending_terminal"
	PayloadStatusEmitted         PayloadStatus = "emitted"
)

// MergedPayload holds the union of every signal the enricher has merged for a
// given proxy-derived InvocationID. The PUT at emission time is rebuilt from
// these fields; the store itself never persists a full analytics.Invocation.
type MergedPayload struct {
	Blob            *blobstats.Snapshot `json:"blob,omitempty"`
	HitRate         float32             `json:"hit_rate,omitempty"`
	HitRateKnown    bool                `json:"hit_rate_known,omitempty"`
	Action          string              `json:"action,omitempty"`
	Scheme          string              `json:"scheme,omitempty"`
	Command         string              `json:"command,omitempty"`
	FullCommand     string              `json:"full_command,omitempty"`
	Start           time.Time           `json:"start,omitempty"`
	Stop            time.Time           `json:"stop,omitempty"`
	DurationMs      int64               `json:"duration_ms,omitempty"`
	LogOutcome      string              `json:"log_outcome,omitempty"`
	XcresultSummary string              `json:"xcresult_summary,omitempty"`
	Success         bool                `json:"success,omitempty"`
	SuccessKnown    bool                `json:"success_known,omitempty"`
	ErrorMessage    string              `json:"error_message,omitempty"`
}

// PayloadRecord is one NDJSON line — one proxy-derived InvocationID's merged
// signal set.
type PayloadRecord struct {
	InvocationID  string        `json:"invocation_id"`
	LastUpdatedAt time.Time     `json:"last_updated_at"`
	Status        PayloadStatus `json:"status"`
	Merged        MergedPayload `json:"merged"`
}

// PayloadStore is the NDJSON merge ledger the enricher upserts against on
// every signal (sidecar update, plist arrival, xcactivitylog metrics parse,
// xcresult parse). Last-write-wins per InvocationID on disk; readers collapse
// duplicate lines in memory.
type PayloadStore struct {
	Path string

	mu sync.Mutex
}

// Load reads every record from disk, collapsing duplicates by InvocationID
// (last wins). Missing file returns nil, nil. Malformed lines are skipped.
func (s *PayloadStore) Load() ([]PayloadRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadLocked()
}

func (s *PayloadStore) loadLocked() ([]PayloadRecord, error) {
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("open payload store: %w", err)
	}
	defer f.Close()

	byID := make(map[string]PayloadRecord)
	order := make([]string, 0)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)

	for scanner.Scan() {
		var r PayloadRecord
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			continue
		}

		if r.InvocationID == "" {
			continue
		}

		if _, seen := byID[r.InvocationID]; !seen {
			order = append(order, r.InvocationID)
		}

		byID[r.InvocationID] = r
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan payload store: %w", err)
	}

	out := make([]PayloadRecord, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}

	return out, nil
}

// Upsert merges the record with any existing in-memory state for its
// InvocationID and appends the merged line. LastUpdatedAt is overwritten with
// now (defaults to time.Now when the caller supplies a zero value).
func (s *PayloadStore) Upsert(rec PayloadRecord) error {
	if rec.InvocationID == "" {
		return errors.New("payload store upsert: empty invocation id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if rec.LastUpdatedAt.IsZero() {
		rec.LastUpdatedAt = time.Now().UTC()
	} else {
		rec.LastUpdatedAt = rec.LastUpdatedAt.UTC()
	}

	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("mkdir payload store dir: %w", err)
	}

	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open payload store append: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal payload record: %w", err)
	}

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write payload record: %w", err)
	}

	return nil
}

// Remove drops every entry for the given InvocationID by atomically
// rewriting the ledger without them. No-op when the ID is absent.
func (s *PayloadStore) Remove(invocationID string) error {
	if invocationID == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadLocked()
	if err != nil {
		return err
	}

	kept := make([]PayloadRecord, 0, len(records))
	removed := false

	for _, r := range records {
		if r.InvocationID == invocationID {
			removed = true

			continue
		}

		kept = append(kept, r)
	}

	if !removed {
		return nil
	}

	return s.writeAtomicLocked(kept)
}

// MergeInto applies src on top of dst in-place, following merge-rules:
//   - Blob snapshots sum via mergeBlobs
//   - Non-zero times narrow Start / widen Stop
//   - Non-empty strings win when dst is empty; later signals overwrite
//     Command/FullCommand (these come from the authoritative manifest)
//   - DurationMs: widen (prefer the larger of the two)
//   - Bool flags: later known wins.
func MergeInto(dst *MergedPayload, src MergedPayload) {
	dst.Blob = mergeBlobs(dst.Blob, src.Blob, nil)

	if src.HitRateKnown {
		dst.HitRate = src.HitRate
		dst.HitRateKnown = true
	}

	if src.Action != "" {
		dst.Action = src.Action
	}
	if src.Scheme != "" {
		dst.Scheme = src.Scheme
	}
	if src.Command != "" {
		dst.Command = src.Command
	}
	if src.FullCommand != "" {
		dst.FullCommand = src.FullCommand
	}

	if !src.Start.IsZero() {
		if dst.Start.IsZero() || src.Start.Before(dst.Start) {
			dst.Start = src.Start
		}
	}
	if !src.Stop.IsZero() {
		if dst.Stop.IsZero() || src.Stop.After(dst.Stop) {
			dst.Stop = src.Stop
		}
	}
	if src.DurationMs > dst.DurationMs {
		dst.DurationMs = src.DurationMs
	}

	if src.LogOutcome != "" {
		dst.LogOutcome = src.LogOutcome
	}
	if src.XcresultSummary != "" {
		dst.XcresultSummary = src.XcresultSummary
	}

	if src.SuccessKnown {
		dst.Success = src.Success
		dst.SuccessKnown = true
	}
	if src.ErrorMessage != "" {
		dst.ErrorMessage = src.ErrorMessage
	}
}

// generic helper would need reflect / json.RawMessage indirection just to share ~40 lines.
//
//nolint:dupl // NDJSON writeAtomic is intentionally per-type — extracting to a
func (s *PayloadStore) writeAtomicLocked(records []PayloadRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("mkdir payload store dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".payload-*.tmp")
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

			return fmt.Errorf("encode payload record: %w", err)
		}
	}

	if err := w.Flush(); err != nil {
		tmp.Close()

		return fmt.Errorf("flush payload store: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Rename(tmpPath, s.Path); err != nil {
		return fmt.Errorf("rename payload store: %w", err)
	}

	return nil
}
