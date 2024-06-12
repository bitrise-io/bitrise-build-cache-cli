package kv

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/bitrise-io/bitrise-build-cache-cli/proto/kv_storage"
)

type Client struct {
	bytestreamClient bytestream.ByteStreamClient
	bitriseKVClient  kv_storage.KVStorageClient
	clientName       string
	token            string
}

type NewClientParams struct {
	UseInsecure bool
	Host        string
	DialTimeout time.Duration
	ClientName  string
	Token       string
}

func NewClient(ctx context.Context, p NewClientParams) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, p.DialTimeout)
	defer cancel()
	creds := credentials.NewTLS(&tls.Config{})
	if p.UseInsecure {
		creds = insecure.NewCredentials()
	}
	transportOpt := grpc.WithTransportCredentials(creds)
	conn, err := grpc.DialContext(ctx, p.Host, transportOpt)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", p.Host, err)
	}

	return &Client{
		bytestreamClient: bytestream.NewByteStreamClient(conn),
		bitriseKVClient:  kv_storage.NewKVStorageClient(conn),
		clientName:       p.ClientName,
		token:            p.Token,
	}, nil
}

type writer struct {
	stream       bytestream.ByteStream_WriteClient
	resourceName string
	offset       int64
	fileSize     int64
}

func (w *writer) Write(p []byte) (int, error) {
	req := &bytestream.WriteRequest{
		ResourceName: w.resourceName,
		WriteOffset:  w.offset,
		Data:         p,
		FinishWrite:  w.offset+int64(len(p)) >= w.fileSize,
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

func (w *writer) Close() error {
	_, err := w.stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("close stream: %w", err)
	}
	return nil
}

type reader struct {
	stream bytestream.ByteStream_ReadClient
	buf    bytes.Buffer
}

func (r *reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	bufLen := r.buf.Len()
	if bufLen > 0 {
		n, _ := r.buf.Read(p) // this will never fail
		return n, nil
	}
	r.buf.Reset()

	resp, err := r.stream.Recv()
	switch {
	case errors.Is(err, io.EOF):
		return 0, io.EOF
	case err != nil:
		return 0, fmt.Errorf("stream receive: %w", err)
	}

	n := copy(p, resp.Data)
	if n == len(resp.Data) {
		return n, nil
	}

	unwritenData := resp.Data[n:]
	_, _ = r.buf.Write(unwritenData) // this will never fail

	return n, nil
}

func (r *reader) Close() error {
	r.buf.Reset()
	return nil
}
