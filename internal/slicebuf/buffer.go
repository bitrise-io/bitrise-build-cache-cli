package slicebuf

import (
	"errors"
	"io"
	"slices"
)

type Buffer struct {
	data       []byte
	readOffset int64
}

func NewBuffer() *Buffer {
	return &Buffer{}
}

func NewBufferWithData(data []byte) *Buffer {
	return &Buffer{data: slices.Clone(data)}
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)

	return len(p), nil
}

func (b *Buffer) WriteTo(w io.Writer) (int64, error) {
	nn, err := w.Write(b.data[b.readOffset:])
	if err != nil {
		return int64(nn), err
	}

	if nn != len(b.data)-int(b.readOffset) {
		return int64(nn), io.ErrShortWrite
	}

	b.readOffset += int64(nn)

	return int64(nn), nil
}

func (b *Buffer) Read(p []byte) (int, error) {
	if b.readOffset >= int64(len(b.data)) {
		return 0, io.EOF
	}

	n := copy(p, b.data[b.readOffset:])
	b.readOffset += int64(n)

	return n, nil
}

func (b *Buffer) Seek(offset int64, whence int) (int64, error) {
	var abs int64

	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = b.readOffset + offset
	case io.SeekEnd:
		abs = int64(len(b.data)) + offset
	default:
		return 0, errors.New("invalid whence")
	}

	if abs < 0 {
		return 0, errors.New("negative position")
	}

	if abs > int64(len(b.data)) {
		return 0, errors.New("seek position exceeds buffer length")
	}

	b.readOffset = abs

	return abs, nil
}

func (b *Buffer) Len() int {
	return len(b.data)
}
