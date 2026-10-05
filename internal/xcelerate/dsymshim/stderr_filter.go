package dsymshim

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"sync"
)

// StderrFilter is an io.WriteCloser that pass-through writes to the underlying
// writer, except for the paired dsymutil warnings the plugin would have resolved:
//
//	warning: 0~<base64>==: No such file or directory
//	note: while processing 0~<base64>==
//
// Those two lines are dropped together — defence-in-depth for a partial
// resolution case where some CAS ids survive the plugin. Any other dsymutil
// stderr passes through byte-for-byte.
type StderrFilter struct {
	w io.Writer

	mu          sync.Mutex
	pending     []byte
	heldWarning []byte
	hasHeld     bool
	paired      int
	ids         int
}

// FilterResult reports the aggregate counts after Close.
type FilterResult struct {
	FilteredPaired int // number of warning+note pairs stripped
	ObservedCASIDs int // number of distinct CAS ids seen (warnings observed, including unpaired)
}

var (
	//nolint:gochecknoglobals // compiled once at package init
	reWarning = regexp.MustCompile(`^warning: (?:[^:\n]*/)?0~[A-Za-z0-9+/_=\-]+==: No such file or directory\r?$`)
	//nolint:gochecknoglobals
	reNote = regexp.MustCompile(`^note: while processing (?:[^\n]*/)?0~[A-Za-z0-9+/_=\-]+==\r?$`)
)

// NewStderrFilter wraps w with the paired-warning filter.
func NewStderrFilter(w io.Writer) *StderrFilter {
	return &StderrFilter{w: w}
}

// Write implements io.Writer. The filter is line-oriented: data is buffered
// until a newline is seen, so a chunk boundary mid-line does not misfire.
func (f *StderrFilter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	n := len(p)
	f.pending = append(f.pending, p...)

	for {
		nl := bytes.IndexByte(f.pending, '\n')
		if nl < 0 {
			break
		}

		line := f.pending[:nl]
		rest := f.pending[nl+1:]

		if err := f.emitLine(line); err != nil {
			f.pending = rest

			return n, err
		}

		f.pending = rest
	}

	return n, nil
}

// Close flushes any trailing (newline-less) buffered content through the filter
// and returns the aggregate counts.
func (f *StderrFilter) Close() FilterResult {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.pending) > 0 {
		_ = f.emitLine(f.pending)
		f.pending = nil
	}

	if f.hasHeld {
		_ = f.rawWriteLine(f.heldWarning)
		f.hasHeld = false
	}

	return FilterResult{
		FilteredPaired: f.paired,
		ObservedCASIDs: f.ids,
	}
}

func (f *StderrFilter) emitLine(line []byte) error {
	// Warning first: observe the id, hold the warning until we see whether the
	// paired note follows. If the next line matches the note, drop both.
	// If it does not match, flush the warning + the next line normally.
	// Dsymutil writes the two lines back-to-back in all observed cases
	// (see /tmp/aci-5540-repro/B2-on.log 1948–1953).
	if reWarning.Match(line) {
		f.ids++
		// Hold this warning until the next line is examined.
		f.heldWarning = append(f.heldWarning[:0], line...)
		f.hasHeld = true

		return nil
	}

	if f.hasHeld {
		if reNote.Match(line) {
			f.paired++
			f.hasHeld = false

			return nil
		}
		// Not the paired note — flush the warning first, then this line normally.
		if err := f.rawWriteLine(f.heldWarning); err != nil {
			f.hasHeld = false

			return err
		}
		f.hasHeld = false
	}

	return f.rawWriteLine(line)
}

func (f *StderrFilter) rawWriteLine(line []byte) error {
	if _, err := f.w.Write(line); err != nil {
		return fmt.Errorf("write filtered stderr: %w", err)
	}

	if _, err := f.w.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("write filtered stderr newline: %w", err)
	}

	return nil
}

// Private — held warning
//
// We keep this on the struct, not in a method local, so Close can flush any
// trailing warning that was never followed by a note (process ended before
// the paired line was emitted).

var _ io.Writer = (*StderrFilter)(nil)
