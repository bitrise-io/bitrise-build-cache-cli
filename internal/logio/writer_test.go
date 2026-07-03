//go:build unit

package logio_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/logio"
)

// captureLogger satisfies the subset of go-utils log.Logger that Writer
// exercises (Printf). Other methods are no-ops.
type captureLogger struct {
	lines []string
}

func (c *captureLogger) Printf(format string, args ...any) {
	c.lines = append(c.lines, strings.TrimSuffix(fmt.Sprintf(format, args...), "\n"))
}

func (c *captureLogger) Println()               {}
func (c *captureLogger) Debugf(string, ...any)  {}
func (c *captureLogger) Infof(string, ...any)   {}
func (c *captureLogger) Warnf(string, ...any)   {}
func (c *captureLogger) Errorf(string, ...any)  {}
func (c *captureLogger) TDebugf(string, ...any) {}
func (c *captureLogger) TInfof(string, ...any)  {}
func (c *captureLogger) TWarnf(string, ...any)  {}
func (c *captureLogger) TPrintf(string, ...any) {}
func (c *captureLogger) TErrorf(string, ...any) {}
func (c *captureLogger) Donef(string, ...any)   {}
func (c *captureLogger) TDonef(string, ...any)  {}
func (c *captureLogger) EnableDebugLog(bool)    {}

func TestWriter_splitsLinesOnNewline(t *testing.T) {
	c := &captureLogger{}
	w := logio.NewWriter(c)

	n, err := w.Write([]byte("first\nsecond\nthird\n"))
	require.NoError(t, err)
	assert.Equal(t, 19, n)
	assert.Equal(t, []string{"first", "second", "third"}, c.lines)
}

func TestWriter_buffersAcrossWritesUntilNewline(t *testing.T) {
	c := &captureLogger{}
	w := logio.NewWriter(c)

	_, err := w.Write([]byte("part1"))
	require.NoError(t, err)
	assert.Empty(t, c.lines, "no newline yet → no log line")

	_, err = w.Write([]byte("-part2\n"))
	require.NoError(t, err)
	assert.Equal(t, []string{"part1-part2"}, c.lines)
}

func TestWriter_flushEmitsResidual(t *testing.T) {
	c := &captureLogger{}
	w := logio.NewWriter(c)

	_, err := w.Write([]byte("no-newline-ending"))
	require.NoError(t, err)
	assert.Empty(t, c.lines)

	w.Flush()
	assert.Equal(t, []string{"no-newline-ending"}, c.lines)
}

func TestWriter_flushEmptyBufferIsNoOp(t *testing.T) {
	c := &captureLogger{}
	w := logio.NewWriter(c)

	w.Flush()
	assert.Empty(t, c.lines)
}

func TestWriter_multiLineThenResidual(t *testing.T) {
	c := &captureLogger{}
	w := logio.NewWriter(c)

	_, err := w.Write([]byte("a\nb\nc"))
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, c.lines)

	w.Flush()
	assert.Equal(t, []string{"a", "b", "c"}, c.lines)
}
