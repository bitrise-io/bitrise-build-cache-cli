// Package invocations is the shared local invocation log under ~/.local/state/bitrise-build-cache/invocations/<YYYY-MM-DD>.ndjson — schema in docs/local-invocation-log.md.
package invocations

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/paths"
)

type Source string

const (
	SourceLocal Source = "local"
	SourceCI    Source = "ci"
)

type Tool string

const (
	ToolXcode  Tool = "xcode"
	ToolGradle Tool = "gradle"
	ToolBazel  Tool = "bazel"
	ToolCcache Tool = "ccache"
	ToolRN     Tool = "rn"
)

type Record struct {
	InvocationID string    `json:"invocation_id"`
	Command      string    `json:"command"`
	Tool         Tool      `json:"tool"`
	ToolVersion  string    `json:"tool_version,omitempty"`
	CLIVersion   string    `json:"cli_version"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at,omitzero"`
	ExitCode     int       `json:"exit_code"`
	Source       Source    `json:"source"`
}

const (
	dayLayout        = "2006-01-02"
	recordSizeLimit  = 4096
	truncSuffix      = "… [truncated]"
	defaultRetention = 30 * 24 * time.Hour
	sweepInterval    = 24 * time.Hour
	sweepMarkerName  = ".last-sweep"
)

type Writer struct {
	Paths paths.Paths
	Clock func() time.Time
}

func NewWriter(p paths.Paths) *Writer {
	return &Writer{Paths: p}
}

func (w *Writer) Append(rec Record) error {
	line, err := encodeRecord(rec)
	if err != nil {
		return err
	}

	if len(line) > recordSizeLimit {
		line, err = encodeWithTruncatedCommand(rec, len(line))
		if err != nil {
			return err
		}
	}

	if err := os.MkdirAll(w.Paths.InvocationsDir(), 0o755); err != nil {
		return fmt.Errorf("mkdir invocations dir: %w", err)
	}

	day := w.now().UTC().Format(dayLayout)
	path := w.Paths.InvocationsFile(day)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open invocation log: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write invocation record: %w", err)
	}

	_ = w.maybeSweep()

	return nil
}

func encodeWithTruncatedCommand(rec Record, encodedLen int) ([]byte, error) {
	orig := rec.Command
	overshoot := encodedLen - recordSizeLimit
	room := len(orig) - overshoot - len(truncSuffix) - 16
	if room < 0 {
		return nil, fmt.Errorf("record %d bytes exceeds atomic-append limit %d even after truncating command", encodedLen, recordSizeLimit)
	}

	for range 3 {
		rec.Command = orig[:room] + truncSuffix
		line, err := encodeRecord(rec)
		if err != nil {
			return nil, err
		}

		if len(line) <= recordSizeLimit {
			return line, nil
		}

		room -= len(line) - recordSizeLimit
		if room < 0 {
			break
		}
	}

	return nil, fmt.Errorf("record still exceeds atomic-append limit %d after 3 truncation attempts", recordSizeLimit)
}

func (w *Writer) maybeSweep() error {
	marker := filepath.Join(w.Paths.InvocationsDir(), sweepMarkerName)
	if info, err := os.Stat(marker); err == nil && w.now().Sub(info.ModTime()) < sweepInterval {
		return nil
	}

	if _, err := Sweep(w.Paths, defaultRetention, w.now()); err != nil {
		return err
	}

	now := w.now()
	_ = os.Chtimes(marker, now, now)
	_ = os.WriteFile(marker, nil, 0o600)

	return nil
}

func (w *Writer) now() time.Time {
	if w.Clock != nil {
		return w.Clock()
	}

	return time.Now()
}

func encodeRecord(rec Record) ([]byte, error) {
	b, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("marshal record: %w", err)
	}

	return append(b, '\n'), nil
}

type Reader struct {
	Paths paths.Paths
}

func NewReader(p paths.Paths) *Reader {
	return &Reader{Paths: p}
}

func (r *Reader) Recent(n int) ([]Record, error) {
	if n <= 0 {
		return nil, nil
	}

	files, err := listDailyFiles(r.Paths.InvocationsDir())
	if err != nil {
		return nil, err
	}

	var out []Record
	for i := len(files) - 1; i >= 0; i-- {
		recs, err := readNDJSON(files[i])
		if err != nil {
			return nil, err
		}
		out = append(recs, out...)

		if len(out) >= n {
			return out[len(out)-n:], nil
		}
	}

	return out, nil
}

func Sweep(p paths.Paths, retention time.Duration, now time.Time) (int, error) {
	files, err := listDailyFiles(p.InvocationsDir())
	if err != nil {
		return 0, err
	}

	cutoff := now.Add(-retention).Truncate(24 * time.Hour)
	removed := 0

	for _, f := range files {
		day := strings.TrimSuffix(filepath.Base(f), ".ndjson")

		t, err := time.Parse(dayLayout, day)
		if err != nil {
			continue
		}

		if t.Before(cutoff) {
			if err := os.Remove(f); err != nil {
				return removed, fmt.Errorf("remove %s: %w", f, err)
			}

			removed++
		}
	}

	return removed, nil
}

func listDailyFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read invocations dir: %w", err)
	}

	out := make([]string, 0, len(entries))

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		if !strings.HasSuffix(name, ".ndjson") {
			continue
		}

		out = append(out, filepath.Join(dir, name))
	}

	sort.Strings(out)

	return out, nil
}

func readNDJSON(path string) ([]Record, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	trimmed := strings.TrimRight(string(b), "\n")
	out := make([]Record, 0, strings.Count(trimmed, "\n")+1)

	for line := range strings.SplitSeq(trimmed, "\n") {
		if line == "" {
			continue
		}

		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("parse line in %s: %w", path, err)
		}

		out = append(out, rec)
	}

	return out, nil
}
