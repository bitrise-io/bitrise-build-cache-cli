// Package logtail provides tail-like streaming of one or more log files.
// It is used by the ccache storage-helper and xcelerate proxy log commands.
package logtail

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	defaultPollInterval = 100 * time.Millisecond
	// scanBufSize bounds how far from EOF we seek when reading the last N lines.
	// 32 KB covers ~100 typical log lines.
	scanBufSize = 32 * 1024
)

// Source is a named log file to read.
type Source struct {
	// Label is emitted as the "source" field in JSON output and the prefix
	// in text output (e.g. "out", "err").
	Label string
	// Path is the absolute file path. A missing file is silently skipped in
	// non-follow mode and waited on in follow mode.
	Path string
}

// Opts configures Tail behaviour.
type Opts struct {
	// Lines is the number of existing trailing lines to emit before following.
	// 0 prints nothing; negative prints everything.
	Lines int
	// Follow streams new content after the initial Lines are printed,
	// until ctx is cancelled.
	Follow bool
	// JSON wraps each line in a {"source","line","ts"} JSON object.
	JSON bool
	// PollInterval controls how often Follow re-reads the file for new bytes.
	// Defaults to 100 ms.
	PollInterval time.Duration
}

// jsonLine is the wire shape for JSON-line output.
type jsonLine struct {
	Source string `json:"source"`
	Line   string `json:"line"`
	TS     string `json:"ts"`
}

// MostRecentGlob returns the path of the most recently modified file matching
// pattern (a filepath.Glob pattern). Returns ("", nil) when no files match.
func MostRecentGlob(pattern string) (string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob %q: %w", pattern, err)
	}
	if len(matches) == 0 {
		return "", nil
	}

	sort.Slice(matches, func(i, j int) bool {
		si, _ := os.Stat(matches[i])
		sj, _ := os.Stat(matches[j])
		if si == nil {
			return false
		}
		if sj == nil {
			return true
		}

		return si.ModTime().After(sj.ModTime())
	})

	return matches[0], nil
}

// Tail emits the last opts.Lines from each source and, when opts.Follow is
// true, streams new content until ctx is cancelled.
func Tail(ctx context.Context, out io.Writer, sources []Source, opts Opts) error {
	if opts.PollInterval == 0 {
		opts.PollInterval = defaultPollInterval
	}

	mu := &sync.Mutex{}
	emit := makeEmitter(out, mu, opts.JSON)

	offsets := make([]int64, len(sources))

	for i, src := range sources {
		offset, lines, err := readLastN(src.Path, opts.Lines)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read %s log: %w", src.Label, err)
		}
		for _, l := range lines {
			emit(src.Label, l)
		}
		offsets[i] = offset
	}

	if !opts.Follow {
		return nil
	}

	var wg sync.WaitGroup

	for i, src := range sources {
		wg.Add(1)
		go func(src Source, offset int64) {
			defer wg.Done()
			followFile(ctx, src, offset, opts.PollInterval, emit)
		}(src, offsets[i])
	}

	wg.Wait()

	return nil
}

// ---------------------------------------------------------------------------
// Private
// ---------------------------------------------------------------------------

func makeEmitter(out io.Writer, mu *sync.Mutex, asJSON bool) func(source, line string) {
	return func(source, line string) {
		mu.Lock()
		defer mu.Unlock()

		if asJSON {
			b, _ := json.Marshal(jsonLine{
				Source: source,
				Line:   line,
				TS:     time.Now().UTC().Format(time.RFC3339Nano),
			})
			fmt.Fprintf(out, "%s\n", b)

			return
		}

		fmt.Fprintf(out, "[%s] %s\n", source, line)
	}
}

// readLastN returns the file size at read time (used as the follow offset) and
// the last n lines. Returns (0, nil, nil) when the file does not exist.
func readLastN(path string, n int) (int64, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, nil, fmt.Errorf("seek end: %w", err)
	}

	if n == 0 {
		return size, nil, nil
	}

	start := size - scanBufSize
	if start < 0 {
		start = 0
	}

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return 0, nil, fmt.Errorf("seek start: %w", err)
	}

	scanner := bufio.NewScanner(f)

	if start > 0 {
		scanner.Scan() // discard partial first line caused by mid-line seek
	}

	var all []string

	for scanner.Scan() {
		all = append(all, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return 0, nil, fmt.Errorf("scan: %w", err)
	}

	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}

	return size, all, nil
}

func followFile(ctx context.Context, src Source, offset int64, interval time.Duration, emit func(string, string)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			offset = readNew(src, offset, emit)
		}
	}
}

func readNew(src Source, offset int64, emit func(string, string)) int64 {
	f, err := os.Open(src.Path)
	if err != nil {
		return offset // file not yet created or temporarily gone
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil || fi.Size() <= offset {
		return offset
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		emit(src.Label, scanner.Text())
	}

	newOffset, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return offset
	}

	return newOffset
}
