// Package logio bridges io.Writer consumers (subprocess stdout, library
// writers that expect an io.Writer sink) onto a go-utils log.Logger, splitting
// the byte stream into per-line log entries.
package logio

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
)

// Writer is an io.Writer that buffers bytes and dispatches one log.Logger.Printf
// call per newline-delimited line. Residual bytes (no trailing newline) wait
// until the next Write supplies one or Flush is called.
type Writer struct {
	logger log.Logger
	buf    bytes.Buffer
}

// NewWriter wraps logger as an io.Writer.
func NewWriter(logger log.Logger) *Writer {
	return &Writer{logger: logger}
}

// Write satisfies io.Writer. Always returns len(p), nil — the underlying
// logger is fire-and-forget so write errors are not propagated.
func (w *Writer) Write(p []byte) (int, error) {
	w.buf.Write(p)

	for {
		line, err := w.buf.ReadString('\n')
		if errors.Is(err, io.EOF) {
			w.buf.WriteString(line)

			break
		}

		w.logger.Printf("%s", strings.TrimRight(line, "\n"))
	}

	return len(p), nil
}

// Flush emits any buffered residual that lacks a trailing newline. Call once
// the source has finished writing so the last partial line isn't dropped.
func (w *Writer) Flush() {
	if w.buf.Len() == 0 {
		return
	}

	w.logger.Printf("%s", strings.TrimRight(w.buf.String(), "\n"))
	w.buf.Reset()
}
