// Package childstats implements a local file-based ledger where child
// invocations record their cache hit rates so the parent invocation can
// aggregate them at the end of a wrapper run (e.g. react-native).
//
// The ledger lives under ~/.bitrise/cache/invocations/<parent-id>/
// with one <child-id>.childstats.json file per child invocation. Writes
// are atomic (tmp file + rename). The path contract is stable and
// cross-language: the gradle plugin writes files to the same location.
//
// The .childstats.json suffix leaves the per-parent directory free for
// other future per-invocation ledgers (with their own suffixes) without
// path collisions.
package childstats

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	// SchemaVersion is the current ledger entry schema version.
	// Readers must tolerate entries with any schema version.
	SchemaVersion = 1

	// BenchmarkPhaseBaseline identifies entries written during a baseline
	// benchmark run (cache disabled). Included in the aggregate at their
	// reported hit rate (typically 0%) and logged so the user can see the
	// baseline drag on the average.
	BenchmarkPhaseBaseline = "baseline"

	// EntryFileSuffix is the suffix used for ledger entry files. The
	// suffix namespaces this ledger so additional per-invocation ledgers
	// can coexist under the same parent directory.
	EntryFileSuffix = ".childstats.json"

	// DefaultSweepTTL is how long a parent ledger directory is kept before
	// Sweep considers it stale. Long enough to outlast any real build;
	// short enough that orphaned dirs (crashed wrappers, aborted steps)
	// do not accumulate forever.
	DefaultSweepTTL = 7 * 24 * time.Hour
)

// Entry is a single child invocation's ledger record.
type Entry struct {
	ChildInvocationID  string  `json:"child_invocation_id"`
	ParentInvocationID string  `json:"parent_invocation_id"`
	BuildTool          string  `json:"build_tool"`
	HitRate            float32 `json:"hit_rate"`
	Hits               int64   `json:"hits,omitempty"`
	Total              int64   `json:"total,omitempty"`
	BenchmarkPhase     string  `json:"benchmark_phase,omitempty"`
	// Failed indicates the child invocation reported a failure. Default is
	// false (no failure) for legacy entries that don't carry the field —
	// writers that can detect failure must set this explicitly.
	Failed        bool      `json:"failed,omitempty"`
	WrittenAt     time.Time `json:"written_at"`
	SchemaVersion int       `json:"schema_version"`
}

// LedgerDir returns the directory for a parent invocation's child entries.
func LedgerDir(parentID string) string {
	return filepath.Join(ledgerRoot(), parentID)
}

// LedgerPath returns the path of a single child entry file.
func LedgerPath(parentID, childID string) string {
	return filepath.Join(LedgerDir(parentID), childID+EntryFileSuffix)
}

// Writer persists ledger entries.
type Writer struct{}

// NewWriter returns a Writer.
func NewWriter() *Writer {
	return &Writer{}
}

// Write persists entry atomically to the ledger. A missing or empty
// ParentInvocationID or ChildInvocationID causes Write to return nil
// without creating a file (no-op).
func (w *Writer) Write(entry Entry) error {
	if entry.ParentInvocationID == "" || entry.ChildInvocationID == "" {
		return nil
	}

	if entry.SchemaVersion == 0 {
		entry.SchemaVersion = SchemaVersion
	}

	if entry.WrittenAt.IsZero() {
		entry.WrittenAt = time.Now().UTC()
	}

	dir := LedgerDir(entry.ParentInvocationID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create ledger dir: %w", err)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}

	finalPath := filepath.Join(dir, entry.ChildInvocationID+EntryFileSuffix)
	tmp, err := os.CreateTemp(dir, entry.ChildInvocationID+".*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp file: %w", err)
	}

	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)

		return fmt.Errorf("write tmp file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("close tmp file: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("rename ledger entry: %w", err)
	}

	return nil
}

// ToolSummary holds per-build-tool aggregate stats.
type ToolSummary struct {
	// MeanHitRate is the unweighted mean of hit_rate across the tool's children.
	MeanHitRate float32

	// WeightedHitRate is sum(hits) / sum(total) across the tool's children.
	// Zero when no entry reported Total > 0.
	WeightedHitRate float32

	// TotalHits is the sum of Hits across the tool's children.
	TotalHits int64

	// TotalCount is the sum of Total across the tool's children.
	TotalCount int64

	// Count is the number of entries included for this tool.
	Count int
}

// Summary is the aggregated result of all child entries for one parent.
//
// Two hit-rate views are reported: MeanHitRate is a simple unweighted mean
// of each child's reported hit rate (every child weighs the same). WeightedHitRate
// is the cache-wide ratio sum(hits)/sum(total) (every cacheable call weighs the
// same). They can diverge when children process very different numbers of items.
// Callers are expected to surface both so users can interpret the result.
type Summary struct {
	// MeanHitRate is the unweighted mean of hit_rate across included children.
	MeanHitRate float32

	// WeightedHitRate is sum(hits) / sum(total) across included children.
	// Zero when no entry reported Total > 0.
	WeightedHitRate float32

	// TotalHits is the sum of Hits across included children.
	TotalHits int64

	// TotalCount is the sum of Total across included children.
	TotalCount int64

	// ChildCount is the number of entries included in MeanHitRate.
	ChildCount int

	// BaselineCount is the number of entries included that came from a baseline
	// benchmark run. They are NOT excluded from the mean — surfaced here so the
	// caller can warn the user that baseline runs drag the average down.
	BaselineCount int

	// SkippedCount is the number of entries excluded (malformed only).
	SkippedCount int

	// FailedCount is the number of entries that reported Failed=true. Used
	// by parent wrappers to fail the wrapper invocation when any child
	// failed (e.g. an xcodebuild child errored even though the parent script
	// kept running).
	FailedCount int

	// ByTool breaks down the stats per build tool (gradle, ccache, xcode, ...).
	ByTool map[string]ToolSummary
}

// Aggregator reads a parent's ledger directory and produces a Summary.
type Aggregator struct {
	parentID string

	// Logger receives warnings about malformed entries and info logs about
	// baseline entries included in the aggregate. Nil means silent.
	Logger log.Logger
}

// NewAggregator returns an Aggregator for the given parent invocation ID.
func NewAggregator(parentID string) *Aggregator {
	return &Aggregator{parentID: parentID}
}

// Compute reads all entries under the parent's ledger directory and
// returns the aggregated Summary. Missing directory is not an error —
// Summary is returned empty. Malformed entries are skipped and counted
// in SkippedCount; baseline entries are included with their reported
// hit rate (typically 0%) and counted in BaselineCount.
func (a *Aggregator) Compute() (Summary, error) {
	summary := Summary{ByTool: map[string]ToolSummary{}}

	if a.parentID == "" {
		return summary, nil
	}

	dir := LedgerDir(a.parentID)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return summary, nil
		}

		return summary, fmt.Errorf("read ledger dir: %w", err)
	}

	var sum float32
	byToolSums := map[string]float32{}
	byToolCounts := map[string]int{}
	byToolHits := map[string]int64{}
	byToolTotals := map[string]int64{}

	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), EntryFileSuffix) {
			continue
		}

		entryPath := filepath.Join(dir, de.Name())

		entry, err := readEntry(entryPath)
		if err != nil {
			summary.SkippedCount++

			if a.Logger != nil {
				a.Logger.Warnf("Skipping malformed child stats entry %s: %v", entryPath, err)
			}

			continue
		}

		if entry.BenchmarkPhase == BenchmarkPhaseBaseline {
			summary.BaselineCount++

			if a.Logger != nil {
				a.Logger.Infof(
					"Including baseline child invocation %s (build tool: %s, hit rate: %.1f%%) in aggregate",
					entry.ChildInvocationID, entry.BuildTool, entry.HitRate*100,
				)
			}
		}

		sum += entry.HitRate
		summary.ChildCount++
		summary.TotalHits += entry.Hits
		summary.TotalCount += entry.Total

		if entry.Failed {
			summary.FailedCount++
		}

		byToolSums[entry.BuildTool] += entry.HitRate
		byToolCounts[entry.BuildTool]++
		byToolHits[entry.BuildTool] += entry.Hits
		byToolTotals[entry.BuildTool] += entry.Total
	}

	if summary.ChildCount > 0 {
		summary.MeanHitRate = sum / float32(summary.ChildCount)
	}

	if summary.TotalCount > 0 {
		summary.WeightedHitRate = float32(summary.TotalHits) / float32(summary.TotalCount)
	}

	for tool, count := range byToolCounts {
		ts := ToolSummary{
			MeanHitRate: byToolSums[tool] / float32(count),
			TotalHits:   byToolHits[tool],
			TotalCount:  byToolTotals[tool],
			Count:       count,
		}

		if ts.TotalCount > 0 {
			ts.WeightedHitRate = float32(ts.TotalHits) / float32(ts.TotalCount)
		}

		summary.ByTool[tool] = ts
	}

	return summary, nil
}

// Cleanup removes the parent's ledger directory. Safe to call when the
// directory does not exist. Intended to run after Compute, once the
// summary has been consumed.
func (a *Aggregator) Cleanup() error {
	if a.parentID == "" {
		return nil
	}

	if err := os.RemoveAll(LedgerDir(a.parentID)); err != nil {
		return fmt.Errorf("remove ledger dir: %w", err)
	}

	return nil
}

// Sweep removes any parent ledger directory whose most recent modification
// is older than ttl. A missing root directory is not an error. Per-directory
// failures are skipped so one bad entry does not abort the whole sweep.
func Sweep(ttl time.Duration) error {
	root := ledgerRoot()

	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("read invocations root: %w", err)
	}

	cutoff := time.Now().Add(-ttl)

	for _, de := range entries {
		if !de.IsDir() {
			continue
		}

		info, err := de.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(root, de.Name()))
		}
	}

	return nil
}

func ledgerRoot() string {
	return filepath.Join(os.Getenv("HOME"), ".bitrise", "cache", "invocations")
}

func readEntry(path string) (Entry, error) {
	var entry Entry

	f, err := os.Open(path) //nolint:gosec // path is composed from our own ledger dir
	if err != nil {
		return entry, fmt.Errorf("open entry: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return entry, fmt.Errorf("read entry: %w", err)
	}

	if err := json.Unmarshal(data, &entry); err != nil {
		return entry, fmt.Errorf("unmarshal entry: %w", err)
	}

	return entry, nil
}
