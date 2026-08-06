package kv

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/build/bazel/remote/execution/v2"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/kv_storage"
)

//go:generate moq -rm -stub -pkg mocks -out ./mocks/kv_storage.go ./../../../proto/kv_storage KVStorageClient

const (
	defaultDownloadRetry     uint          = 3
	defaultUploadRetry       uint          = 3
	defaultDownloadRetryWait time.Duration = 1 * time.Second
	defaultUploadRetryWait   time.Duration = 1 * time.Second

	keepaliveTime    = 30 * time.Second
	keepaliveTimeout = 10 * time.Second
)

// Channel-pool sizing mirrors the Gradle plugin's ClientBalancer.
var (
	numChannels     = max(2, runtime.NumCPU()/6)
	perChannelLimit = runtime.NumCPU()
)

// AuthSource returns the credentials to use for a single RPC. Implementations
// may cache and refresh transparently; kv.Client re-reads on every call.
type AuthSource interface {
	Get() common.CacheAuthConfig
}

type staticAuthSource struct {
	cfg common.CacheAuthConfig
}

func (s staticAuthSource) Get() common.CacheAuthConfig { return s.cfg }

// channel binds one gRPC connection to its per-channel semaphore and the
// three sub-clients dialed on top of it. The semaphore caps concurrent RPCs on
// this channel so we can never open more streams than HTTP/2 supports without
// waiting on the transport itself.
type channel struct {
	conn               *grpc.ClientConn // nil when test-injected (see NewClient).
	sem                chan struct{}    // nil disables throttling (test-injected mode).
	bitriseKVClient    kv_storage.KVStorageClient
	capabilitiesClient remoteexecution.CapabilitiesClient
	casClient          remoteexecution.ContentAddressableStorageClient
}

func (ch *channel) acquire() {
	if ch.sem == nil {
		return
	}

	ch.sem <- struct{}{}
}

func (ch *channel) release() {
	if ch.sem == nil {
		return
	}

	<-ch.sem
}

type Client struct {
	channels            []*channel
	channelCursor       atomic.Uint64
	clientName          string
	authSource          AuthSource
	cacheConfigMetadata common.CacheConfigMetadata
	logger              log.Logger
	cacheOperationID    string
	invocationID        string
	sessionMutex        sync.Mutex
	downloadRetry       uint
	downloadRetryWait   time.Duration
	uploadRetry         uint
	uploadRetryWait     time.Duration
}

// pickChannel round-robins across channels. Callers must acquire the channel's
// semaphore before use and release it once the RPC (including any streaming
// body) has drained.
func (c *Client) pickChannel() *channel {
	i := c.channelCursor.Add(1) - 1

	return c.channels[i%uint64(len(c.channels))]
}

type NewClientParams struct {
	UseInsecure         bool
	Host                string
	DialTimeout         time.Duration
	ClientName          string
	AuthConfig          common.CacheAuthConfig
	AuthSource          AuthSource // preferred; falls back to AuthConfig when nil
	CacheConfigMetadata common.CacheConfigMetadata
	Logger              log.Logger
	CacheOperationID    string
	BitriseKVClient     kv_storage.KVStorageClient
	CapabilitiesClient  remoteexecution.CapabilitiesClient
	InvocationID        string
	DownloadRetry       uint
	DownloadRetryWait   time.Duration
	UploadRetry         uint
	UploadRetryWait     time.Duration
}

func NewClient(p NewClientParams) (*Client, error) {
	if p.DownloadRetry == 0 {
		p.DownloadRetry = defaultDownloadRetry
	}
	if p.DownloadRetryWait == 0 {
		p.DownloadRetryWait = defaultDownloadRetryWait
	}
	if p.UploadRetry == 0 {
		p.UploadRetry = defaultUploadRetry
	}
	if p.UploadRetryWait == 0 {
		p.UploadRetryWait = defaultUploadRetryWait
	}

	authSource := p.AuthSource
	if authSource == nil {
		authSource = staticAuthSource{cfg: p.AuthConfig}
	}

	channels, err := buildChannels(p)
	if err != nil {
		return nil, err
	}

	return &Client{
		channels:            channels,
		clientName:          p.ClientName,
		authSource:          authSource,
		logger:              p.Logger,
		cacheConfigMetadata: p.CacheConfigMetadata,
		cacheOperationID:    p.CacheOperationID,
		invocationID:        p.InvocationID,
		downloadRetry:       p.DownloadRetry,
		downloadRetryWait:   p.DownloadRetryWait,
		uploadRetry:         p.UploadRetry,
		uploadRetryWait:     p.UploadRetryWait,
	}, nil
}

// buildChannels dials one gRPC connection per channel, each with its own
// per-channel semaphore. Sizing (numChannels, perChannelLimit) is defined at
// package level and mirrors the Gradle plugin's ClientBalancer.
//
// If callers inject a stub (BitriseKVClient or CapabilitiesClient) the pool
// collapses to a single, no-throttle channel that hands the stub straight
// back — keeps every test that only wires one mock working unchanged.
func buildChannels(p NewClientParams) ([]*channel, error) {
	if p.BitriseKVClient != nil || p.CapabilitiesClient != nil {
		return []*channel{{
			bitriseKVClient:    p.BitriseKVClient,
			capabilitiesClient: p.CapabilitiesClient,
		}}, nil
	}

	creds := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	if p.UseInsecure {
		creds = insecure.NewCredentials()
	}

	kaParams := keepalive.ClientParameters{
		Time:                keepaliveTime,
		Timeout:             keepaliveTimeout,
		PermitWithoutStream: true,
	}

	channels := make([]*channel, 0, numChannels)
	for range numChannels {
		conn, err := grpc.NewClient(p.Host,
			grpc.WithTransportCredentials(creds),
			grpc.WithKeepaliveParams(kaParams),
		)
		if err != nil {
			for _, ch := range channels {
				_ = ch.conn.Close()
			}

			return nil, fmt.Errorf("dial %s: %w", p.Host, err)
		}

		channels = append(channels, &channel{
			conn:               conn,
			sem:                make(chan struct{}, perChannelLimit),
			bitriseKVClient:    kv_storage.NewKVStorageClient(conn),
			capabilitiesClient: remoteexecution.NewCapabilitiesClient(conn),
			casClient:          remoteexecution.NewContentAddressableStorageClient(conn),
		})
	}

	return channels, nil
}

func (c *Client) SetLogger(logger log.Logger) {
	c.logger = logger
}

// Close releases every gRPC connection in the pool. Safe to call when the
// client was built with injected stubs — channels without a conn are skipped.
func (c *Client) Close() error {
	var firstErr error
	for _, ch := range c.channels {
		if ch.conn == nil {
			continue
		}

		if err := ch.conn.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close kv grpc conn: %w", err)
		}
	}

	return firstErr
}

type writer struct {
	stream       bytestream.ByteStream_WriteClient
	resourceName string
	offset       int64
	fileSize     int64
	response     *bytestream.WriteResponse
	release      func()
	releaseOnce  sync.Once
}

func (w *writer) Response() *bytestream.WriteResponse {
	return w.response
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

func (w *writer) doRelease() {
	w.releaseOnce.Do(func() {
		if w.release != nil {
			w.release()
		}
	})
}

func (w *writer) Close() error {
	defer w.doRelease()

	var err error
	w.response, err = w.stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("close stream: %w", err)
	}

	return nil
}

type reader struct {
	logger   log.Logger
	stream   bytestream.ByteStream_ReadClient
	metadata sync.Map
	buf      bytes.Buffer

	metadataReady chan struct{}
	release       func()
	releaseOnce   sync.Once
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
		r.readTrailerMetadata()

		return 0, io.EOF
	case err != nil:
		r.readTrailerMetadata()

		return 0, fmt.Errorf("stream receive: %w", err)
	}

	n := copy(p, resp.GetData())
	if n == len(resp.GetData()) {
		return n, nil
	}

	unwrittenData := resp.GetData()[n:]
	_, _ = r.buf.Write(unwrittenData) // this will never fail

	return n, nil
}

func (r *reader) readStreamMetadata() {
	if header, err := r.stream.Header(); err == nil {
		for k, v := range header {
			if len(v) > 0 {
				r.metadata.Store(k, v[0])
			}
		}
	} else {
		r.logger.Errorf("Failed to read stream header: %v", err)
	}

	go func() {
		close(r.metadataReady)
	}()
}

func (r *reader) readTrailerMetadata() {
	if trailer := r.stream.Trailer(); trailer != nil {
		for k, v := range trailer {
			if len(v) > 0 {
				r.metadata.Store(k, v[0])
			}
		}
	}
}

func (r *reader) Metadata() map[string]string {
	<-r.metadataReady
	m := make(map[string]string)
	r.metadata.Range(func(key, value any) bool {
		k, ok1 := key.(string)
		v, ok2 := value.(string)
		if ok1 && ok2 {
			m[k] = v
		}

		return true
	})

	return m
}

func (r *reader) doRelease() {
	r.releaseOnce.Do(func() {
		if r.release != nil {
			r.release()
		}
	})
}

func (r *reader) Close() error {
	r.buf.Reset()
	r.doRelease()

	return nil
}
