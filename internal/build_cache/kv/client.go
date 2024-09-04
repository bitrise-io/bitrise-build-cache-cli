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

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/kv_storage"
	"github.com/bitrise-io/go-utils/v2/log"
)

type Client struct {
	bytestreamClient    bytestream.ByteStreamClient
	bitriseKVClient     kv_storage.KVStorageClient
	capabilitiesClient  remoteexecution.CapabilitiesClient
	casClient           remoteexecution.ContentAddressableStorageClient
	clientName          string
	authConfig          common.CacheAuthConfig
	cacheConfigMetadata common.CacheConfigMetadata
	logger              log.Logger
}

type NewClientParams struct {
	UseInsecure         bool
	Host                string
	DialTimeout         time.Duration
	ClientName          string
	AuthConfig          common.CacheAuthConfig
	CacheConfigMetadata common.CacheConfigMetadata
	Logger              log.Logger
}

func NewClient(ctx context.Context, p NewClientParams) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, p.DialTimeout)
	defer cancel()
	creds := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	if p.UseInsecure {
		creds = insecure.NewCredentials()
	}
	transportOpt := grpc.WithTransportCredentials(creds)
	// nolint: staticcheck
	conn, err := grpc.DialContext(ctx, p.Host, transportOpt)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", p.Host, err)
	}

	return &Client{
		bytestreamClient:    bytestream.NewByteStreamClient(conn),
		bitriseKVClient:     kv_storage.NewKVStorageClient(conn),
		capabilitiesClient:  remoteexecution.NewCapabilitiesClient(conn),
		casClient:           remoteexecution.NewContentAddressableStorageClient(conn),
		clientName:          p.ClientName,
		authConfig:          p.AuthConfig,
		logger:              p.Logger,
		cacheConfigMetadata: p.CacheConfigMetadata,
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

	n := copy(p, resp.GetData())
	if n == len(resp.GetData()) {
		return n, nil
	}

	unwritenData := resp.GetData()[n:]
	_, _ = r.buf.Write(unwritenData) // this will never fail

	return n, nil
}

func (r *reader) Close() error {
	r.buf.Reset()

	return nil
}
