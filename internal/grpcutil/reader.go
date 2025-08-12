package grpcutil

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

type Message interface {
	GetData() []byte
}

type Receiver[T Message] interface {
	Recv() (T, error)
}

type Reader[T Message] struct {
	receiver       Receiver[T]
	buffer         *bytes.Buffer
	BytesProcessed int64 // BytesProcessed is the number raw bytes read from the underlying receiver.
}

func NewReader[T Message](receiver Receiver[T]) *Reader[T] {
	return &Reader[T]{
		receiver:       receiver,
		buffer:         bytes.NewBuffer(nil),
		BytesProcessed: 0,
	}
}

func (gr *Reader[T]) Read(data []byte) (int, error) {
	if gr.buffer.Len() == 0 {
		msg, err := gr.receiver.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, io.EOF
			}

			return 0, fmt.Errorf("recv: %w", err)
		}

		gr.buffer.Write(msg.GetData())
	}

	n, err := gr.buffer.Read(data)
	gr.BytesProcessed += int64(n)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("read buffer: %w", err)
	}

	return n, nil
}

func (gr *Reader[T]) Close() error {
	gr.buffer.Reset()

	return nil
}
