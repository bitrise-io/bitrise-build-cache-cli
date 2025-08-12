package grpcutil

import (
	"errors"
	"fmt"
	"io"

	"google.golang.org/genproto/googleapis/bytestream"
)

const (
	mib                          = 1024 * 1024
	gRPCResponseDefaultChunkSize = 1 * mib
)

type ClientWriter struct {
	stream       bytestream.ByteStream_WriteClient
	resourceName string
	offset       int64
	chunkSize    int
}

func NewClientWriter(stream bytestream.ByteStream_WriteClient, resourceName string, offset int64) *ClientWriter {
	return NewClientWriterWithChunkSize(stream, resourceName, offset, gRPCResponseDefaultChunkSize)
}

func NewClientWriterWithChunkSize(
	stream bytestream.ByteStream_WriteClient,
	resourceName string,
	offset int64,
	chunkSize int,
) *ClientWriter {
	return &ClientWriter{stream: stream, resourceName: resourceName, offset: offset, chunkSize: chunkSize}
}

func (w *ClientWriter) Write(p []byte) (int, error) {
	if len(p) > w.chunkSize {
		p = p[:w.chunkSize]
	}

	req := &bytestream.WriteRequest{
		ResourceName: w.resourceName,
		WriteOffset:  w.offset,
		Data:         p,
		FinishWrite:  false,
	}
	err := w.stream.Send(req)
	switch {
	case errors.Is(err, io.EOF):
		return 0, io.EOF
	case err != nil:
		return 0, fmt.Errorf("send data: %w", err)
	}
	w.offset += int64(len(p))

	return len(p), nil
}

func (w *ClientWriter) Close() error {
	err := w.stream.Send(&bytestream.WriteRequest{
		ResourceName: w.resourceName,
		WriteOffset:  w.offset,
		FinishWrite:  true,
	})
	if err != nil {
		return fmt.Errorf("send finish write: %w", err)
	}

	_, err = w.stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("close stream: %w", err)
	}

	return nil
}

func (w *ClientWriter) Abort() {
	_ = w.stream.CloseSend()
}
