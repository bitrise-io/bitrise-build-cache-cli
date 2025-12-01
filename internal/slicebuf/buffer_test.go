package slicebuf

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuffer_WriteAndRead(t *testing.T) {
	buf := NewBuffer()
	data := []byte("hello world")
	written, err := buf.Write(data)
	require.NoError(t, err)
	require.Equal(t, len(data), written)

	read := make([]byte, len(data))
	n, err := buf.Read(read)
	require.NoError(t, err)
	require.Equal(t, len(data), n)
	require.Equal(t, data, read)

	// Reading again should return EOF
	n, err = buf.Read(read)
	require.Equal(t, 0, n)
	require.Equal(t, io.EOF, err)
}

func TestBuffer_SeekAndRead(t *testing.T) {
	buf := NewBufferWithData([]byte("abcdef"))
	_, err := buf.Seek(2, io.SeekStart)
	require.NoError(t, err)

	read := make([]byte, 2)
	n, err := buf.Read(read)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, []byte("cd"), read)

	// Seek back to start
	_, err = buf.Seek(0, io.SeekStart)
	require.NoError(t, err)
	read = make([]byte, 3)
	n, err = buf.Read(read)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Equal(t, []byte("abc"), read)
}

func TestBuffer_SeekEnd(t *testing.T) {
	buf := NewBufferWithData([]byte("abcdef"))
	_, err := buf.Seek(-2, io.SeekEnd)
	require.NoError(t, err)
	read := make([]byte, 2)
	n, err := buf.Read(read)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, []byte("ef"), read)
}

func TestBuffer_SeekCurrent(t *testing.T) {
	buf := NewBufferWithData([]byte("abcdef"))
	_, err := buf.Seek(1, io.SeekStart)
	require.NoError(t, err)
	_, err = buf.Seek(2, io.SeekCurrent)
	require.NoError(t, err)
	read := make([]byte, 2)
	n, err := buf.Read(read)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, []byte("de"), read)
}

func TestBuffer_SeekInvalid(t *testing.T) {
	buf := NewBufferWithData([]byte("abc"))
	_, err := buf.Seek(-1, io.SeekStart)
	require.Error(t, err)
	_, err = buf.Seek(100, io.SeekStart)
	require.Error(t, err)
	_, err = buf.Seek(0, 12345)
	require.Error(t, err)
}

func TestBuffer_Len(t *testing.T) {
	buf := NewBufferWithData([]byte("abc"))
	require.Equal(t, 3, buf.Len())
	_, err := buf.Write([]byte("de"))
	require.NoError(t, err)
	require.Equal(t, 5, buf.Len())
}

func TestBuffer_WriteTo_AllData(t *testing.T) {
	buf := NewBufferWithData([]byte("hello world"))
	var out bytes.Buffer
	n, err := buf.WriteTo(&out)
	require.NoError(t, err)
	require.Equal(t, int64(len("hello world")), n)
	require.Equal(t, "hello world", out.String())
	// After WriteTo, readOffset should be at end
	require.Equal(t, int64(len("hello world")), buf.readOffset)
}

func TestBuffer_WriteTo_AfterSeek(t *testing.T) {
	buf := NewBufferWithData([]byte("abcdef"))
	_, err := buf.Seek(2, io.SeekStart)
	require.NoError(t, err)
	var out bytes.Buffer
	n, err := buf.WriteTo(&out)
	require.NoError(t, err)
	require.Equal(t, int64(4), n)
	require.Equal(t, "cdef", out.String())
	// After WriteTo, readOffset should be at end
	require.Equal(t, int64(6), buf.readOffset)
}

func TestBuffer_WriteTo_Empty(t *testing.T) {
	buf := NewBuffer()
	var out bytes.Buffer
	n, err := buf.WriteTo(&out)
	require.NoError(t, err)
	require.Equal(t, int64(0), n)
	require.Empty(t, out.String())
}

// writer that returns error after writing some bytes
type errorWriter struct {
	failAfter int
	written   int
}

func (w *errorWriter) Write(p []byte) (int, error) {
	if w.failAfter >= 0 && w.written >= w.failAfter {
		return 0, errors.New("write error")
	}
	toWrite := len(p)
	if w.failAfter >= 0 && w.written+toWrite > w.failAfter {
		toWrite = w.failAfter - w.written
	}
	w.written += toWrite
	if toWrite < len(p) {
		return toWrite, errors.New("write error")
	}

	return toWrite, nil
}

func TestBuffer_WriteTo_Error(t *testing.T) {
	buf := NewBufferWithData([]byte("abcdef"))
	w := &errorWriter{failAfter: 3}
	n, err := buf.WriteTo(w)
	require.Error(t, err)
	require.Equal(t, int64(3), n)
}
