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

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

// DefaultCrashFallbackTimeout bounds how long the single-ID pipeline waits
// for a terminal signal (plist landing + xcactivitylog outcome) after the
// last sidecar signal before falling back to a crash-flagged PUT. 60s is
// generous enough to cover IDE builds whose plist writes trail the last dark
// build by a few seconds; shorter runs still pick up the plist before the
// timer fires.
const DefaultCrashFallbackTimeout = 60 * time.Second

// defaultCompactThreshold is the file-size point past which Upsert folds the
// append-only ledger back into one line per InvocationID. 10 MiB caps the
// cost of a Load() during a long IDE session; last-write-wins semantics keep
// Compact a safe no-op on an already-collapsed ledger.
const defaultCompactThreshold = 10 << 20

// PayloadStatus tracks where a PayloadRecord sits in the single-ID merge-and-
// re-PUT lifecycle: PendingTerminal once the terminal plist or crash-fallback
// is queued, Emitted once the BE PUT lands.
type PayloadStatus string

const (
	PayloadStatusPendingTerminal PayloadStatus = "pending_terminal"
	PayloadStatusEmitted         PayloadStatus = "emitted"
)

// MergedPayload holds the union of every signal the enricher has merged for a
// given proxy-derived InvocationID. The PUT at emission time is rebuilt from
// these fields; the store itself never persists a full analytics.Invocation.
type MergedPayload struct {
	Blob         *blobstats.Snapshot `json:"blob,omitempty"`
	HitRate      float32             `json:"hit_rate,omitempty"`
	HitRateKnown bool                `json:"hit_rate_known,omitempty"`
	Scheme       string              `json:"scheme,omitempty"`
	Command      string              `json:"command,omitempty"`
	FullCommand  string              `json:"full_command,omitempty"`
	Start        time.Time           `json:"start,omitempty"`
	Stop         time.Time           `json:"stop,omitempty"`
	DurationMs   int64               `json:"duration_ms,omitempty"`
	LogOutcome   string              `json:"log_outcome,omitempty"`
	Success      bool                `json:"success,omitempty"`
	SuccessKnown bool                `json:"success_known,omitempty"`
	ErrorMessage string              `json:"error_message,omitempty"`
}

// PayloadRecord is one NDJSON line — one proxy-derived InvocationID's merged
// signal set.
type PayloadRecord struct {
	InvocationID  string        `json:"invocation_id"`
	LastUpdatedAt time.Time     `json:"last_updated_at"`
	Status        PayloadStatus `json:"status"`
	Merged        MergedPayload `json:"merged"`
}

// ToInvocation rebuilds an analytics.Invocation from a MergedPayload keyed
// under invocationID. Used by the crash-fallback re-emit path: when no
// terminal plist lands inside CrashFallbackTimeout, the last-known merged
// signal becomes the BE row. The returned run stats carry Success=false with
// a "wrapperless_crash_fallback" error when the merged payload has no
// positive success signal.
func (p MergedPayload) ToInvocation(invocationID string, cred auth.Credential, md configcommon.CacheConfigMetadata, xcodeVersion, xcodeBuildNumber string) analytics.Invocation {
	success := p.SuccessKnown && p.Success
	errText := p.ErrorMessage
	if !p.SuccessKnown {
		errText = "wrapperless_crash_fallback"
	}

	duration := p.DurationMs
	if duration == 0 && !p.Start.IsZero() && !p.Stop.IsZero() {
		duration = p.Stop.Sub(p.Start).Milliseconds()
	}

	var runErr error
	if errText != "" {
		runErr = errors.New(errText)
	}

	runStats := analytics.InvocationRunStats{
		InvocationDate:   p.Start,
		InvocationID:     invocationID,
		Duration:         duration,
		Command:          p.Command,
		FullCommand:      p.FullCommand,
		Success:          success,
		Error:            runErr,
		XcodeVersion:     xcodeVersion,
		XcodeBuildNumber: xcodeBuildNumber,
		HitRate:          p.HitRate,
		CacheBlobStats:   p.Blob,
	}

	return *analytics.NewInvocation(runStats, cred, md)
}

// PayloadStore is the NDJSON merge ledger the enricher upserts against on
// every signal (sidecar update, plist arrival, xcactivitylog metrics parse,
// xcresult parse). Last-write-wins per InvocationID on disk; readers collapse
// duplicate lines in memory.
//
// CrashFallbackTimeout, when > 0, schedules a per-record AfterFunc (started
// on the first Upsert of each InvocationID) that sends the InvocationID to
// CrashFallbackChannel. The AfterFunc never touches scanMu; the Watcher
// drains the channel at scan start. Cancelled once a terminal upsert
// (Status == Emitted) lands for the same ID.
type PayloadStore struct {
	Path                 string
	CrashFallbackTimeout time.Duration
	CrashFallbackChannel chan<- string
	// CompactThresholdBytes overrides defaultCompactThreshold; the ledger is
	// rewritten to one line per InvocationID the next time Upsert finds the
	// file at or above this size. Zero falls back to defaultCompactThreshold.
	CompactThresholdBytes int64

	mu     sync.Mutex
	timers map[string]*time.Timer
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

	s.manageCrashFallbackTimerLocked(rec)

	if err := s.autoCompactLocked(f); err != nil {
		return err
	}

	return nil
}

// Compact rewrites the ledger keeping only the latest record per
// InvocationID. Safe on a missing file (no-op) and on an already-compact
// ledger (last-write-wins means the rewrite produces the same contents).
func (s *PayloadStore) Compact() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.compactLocked()
}

func (s *PayloadStore) compactLocked() error {
	records, err := s.loadLocked()
	if err != nil {
		return err
	}

	if len(records) == 0 {
		return nil
	}

	return s.writeAtomicLocked(records)
}

// autoCompactLocked rewrites the ledger when the file exceeds the configured
// compaction threshold. Called from Upsert after the append so the newly
// written record is part of the compaction input.
func (s *PayloadStore) autoCompactLocked(f *os.File) error {
	threshold := s.CompactThresholdBytes
	if threshold <= 0 {
		threshold = defaultCompactThreshold
	}

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat payload store: %w", err)
	}

	if info.Size() < threshold {
		return nil
	}

	return s.compactLocked()
}

// manageCrashFallbackTimerLocked schedules or cancels the per-record
// crash-fallback AfterFunc depending on record status. Must be called with
// s.mu held.
func (s *PayloadStore) manageCrashFallbackTimerLocked(rec PayloadRecord) {
	if s.CrashFallbackTimeout <= 0 || s.CrashFallbackChannel == nil {
		return
	}

	if s.timers == nil {
		s.timers = make(map[string]*time.Timer)
	}

	if rec.Status == PayloadStatusEmitted {
		if t, ok := s.timers[rec.InvocationID]; ok {
			t.Stop()
			delete(s.timers, rec.InvocationID)
		}

		return
	}

	// Reset the timer on every non-terminal upsert so the fallback fires
	// relative to the last signal rather than the first.
	if t, ok := s.timers[rec.InvocationID]; ok {
		t.Reset(s.CrashFallbackTimeout)

		return
	}

	id := rec.InvocationID
	s.timers[id] = time.AfterFunc(s.CrashFallbackTimeout, func() {
		// AfterFunc must NOT take s.mu — it enqueues to the channel and the
		// scan loop drains + re-emits under scanMu.
		select {
		case s.CrashFallbackChannel <- id:
		default:
		}
	})
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

	if t, ok := s.timers[invocationID]; ok {
		t.Stop()
		delete(s.timers, invocationID)
	}

	return s.writeAtomicLocked(kept)
}

// writeAtomicLocked rewrites the payload store NDJSON to an adjacent tempfile
// and rename-swaps it in. Intentionally per-type: a generic helper would need
// reflect / json.RawMessage indirection just to share ~40 lines.
//
//nolint:dupl // per-type NDJSON writer; see package-level rationale above
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
