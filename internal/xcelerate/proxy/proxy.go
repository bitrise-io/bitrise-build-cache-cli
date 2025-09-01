package proxy

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/hash"
	llvmcas "github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/cas"
	llvmkv "github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/session"
)

var (
	_ llvmcas.CASDBServiceServer = (*Proxy)(nil)
	_ llvmkv.KeyValueDBServer    = (*Proxy)(nil)
	_ session.SessionServer      = (*Proxy)(nil)
)

type Proxy struct {
	llvmcas.UnimplementedCASDBServiceServer
	llvmkv.UnimplementedKeyValueDBServer
	session.UnimplementedSessionServer

	kvClient                *kv.Client
	sessionMutex            sync.Mutex
	capabilitiesCalled      bool
	statsCollector          *statsCollector
	skipGetCapabilitiesCall []grpc.ServiceDesc
	logger                  log.Logger
}

func NewProxy(
	kvClient *kv.Client,
	logger log.Logger,
) *grpc.Server {
	//nolint:exhaustruct
	proxy := &Proxy{
		kvClient:       kvClient,
		statsCollector: newStatsCollector(),
		logger:         logger,
		skipGetCapabilitiesCall: []grpc.ServiceDesc{
			session.Session_ServiceDesc, // skip GetCapabilities call for session service methods
		},
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(func(
			ctx context.Context,
			req any,
			info *grpc.UnaryServerInfo,
			handler grpc.UnaryHandler,
		) (any, error) {
			logger.TDebugf(info.FullMethod)

			if err := proxy.callGetCapabilities(info, ctx); err != nil {
				return nil, err
			}

			return handler(ctx, req)
		}),
	)

	llvmcas.RegisterCASDBServiceServer(grpcServer, proxy)
	llvmkv.RegisterKeyValueDBServer(grpcServer, proxy)
	session.RegisterSessionServer(grpcServer, proxy)

	return grpcServer
}

func (p *Proxy) SetSession(_ context.Context, request *session.SetSessionRequest) (*emptypb.Empty, error) {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	p.capabilitiesCalled = false

	p.kvClient.ChangeSession(request.GetInvocationId(), request.GetAppSlug(), request.GetBuildSlug(), request.GetStepSlug())

	p.statsCollector = newStatsCollector()

	p.logger.TInfof("SetSession called with invocation ID: %s, app slug: %s, build slug: %s, step slug: %s",
		request.GetInvocationId(),
		request.GetAppSlug(),
		request.GetBuildSlug(),
		request.GetStepSlug(),
	)

	return &emptypb.Empty{}, nil
}

func (p *Proxy) GetSessionStats(_ context.Context, _ *emptypb.Empty) (*session.GetSessionStatsResponse, error) {
	collectedStats := p.statsCollector.getStats()

	return &session.GetSessionStatsResponse{
		UploadedBytes:   collectedStats.uploadBytes,
		DownloadedBytes: collectedStats.downloadBytes,
		Hits:            collectedStats.hits,
		Misses:          collectedStats.misses,
	}, nil
}

func (p *Proxy) Get(ctx context.Context, request *llvmcas.CASGetRequest) (*llvmcas.CASGetResponse, error) {
	key := createLLVMCasKey(request.GetCasId())

	p.logger.TDebugf("Get called with request: %s", key)

	var hit bool

	start := time.Now()
	defer func() {
		p.logReadCallStats("Get", key, start, hit)
	}()

	errorHandler := func(err error) *llvmcas.CASGetResponse {
		if errors.Is(err, kv.ErrCacheNotFound) {
			p.statsCollector.incrementMisses()

			//nolint:exhaustruct
			return &llvmcas.CASGetResponse{
				Outcome: llvmcas.CASGetResponse_OBJECT_NOT_FOUND,
			}
		}

		p.logger.TErrorf("Get error: %s", err)

		return &llvmcas.CASGetResponse{
			Outcome: llvmcas.CASGetResponse_ERROR,
			Contents: &llvmcas.CASGetResponse_Error{
				Error: &llvmcas.ResponseError{
					Description: err.Error(),
				},
			},
		}
	}

	buffer := bytes.NewBuffer(nil)
	err := p.kvClient.DownloadStream(ctx, buffer, key)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to download data: %w", err)), nil
	}

	p.statsCollector.addDownloadBytes(int64(buffer.Len()))

	data := blob{} //nolint:exhaustruct
	if err := gob.NewDecoder(buffer).Decode(&data); err != nil {
		return errorHandler(fmt.Errorf("failed to decode data: %w", err)), nil
	}
	references := make([]*llvmcas.CASDataID, 0, len(data.References))
	for _, ref := range data.References {
		references = append(references, &llvmcas.CASDataID{
			Id: ref,
		})
	}

	hit = true
	p.statsCollector.incrementHits()

	return &llvmcas.CASGetResponse{
		Outcome: llvmcas.CASGetResponse_SUCCESS,
		Contents: &llvmcas.CASGetResponse_Data{
			Data: &llvmcas.CASObject{
				Blob: &llvmcas.CASBytes{
					Contents: &llvmcas.CASBytes_Data{
						Data: data.Data,
					},
				},
				References: references,
			},
		},
	}, nil
}

func (p *Proxy) Put(ctx context.Context, request *llvmcas.CASPutRequest) (*llvmcas.CASPutResponse, error) {
	p.logger.TDebugf("Put called with references: %s", request.GetData().GetReferences())

	errorHandler := func(err error) *llvmcas.CASPutResponse {
		p.logger.TErrorf("Put error: %s", err)

		return &llvmcas.CASPutResponse{
			Contents: &llvmcas.CASPutResponse_Error{
				Error: &llvmcas.ResponseError{
					Description: err.Error(),
				},
			},
		}
	}

	var key string

	start := time.Now()
	defer func() {
		p.logWriteCallStats("Save", key, start)
	}()

	var data []byte
	if request.GetData().GetBlob().GetFilePath() != "" {
		var err error
		data, err = os.ReadFile(request.GetData().GetBlob().GetFilePath())
		if err != nil {
			return errorHandler(fmt.Errorf("failed to read file %s: %w", request.GetData().GetBlob().GetFilePath(), err)), nil
		}
	} else {
		data = request.GetData().GetBlob().GetData()
	}

	rawData := &blob{
		Data:       data,
		References: make([][]byte, 0, len(request.GetData().GetReferences())),
	}

	for _, ref := range request.GetData().GetReferences() {
		rawData.References = append(rawData.References, ref.GetId())
	}

	hasher := hash.NewBlobHasher(digestFunction)
	buffer := bytes.NewBuffer(nil)

	if err := gob.NewEncoder(io.MultiWriter(hasher, buffer)).Encode(rawData); err != nil {
		return errorHandler(fmt.Errorf("failed to encode data: %w", err)), nil
	}

	casId := &llvmcas.CASDataID{
		Id: hasher.Sum(nil),
	}
	key = createLLVMCasKey(casId)

	p.logger.TDebugf("Put: CAS ID: %s", key)

	size := int64(buffer.Len())
	p.statsCollector.addUploadBytes(size)

	err := p.kvClient.UploadStreamToBuildCache(ctx, buffer, key, size)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to upload data: %w", err)), nil
	}

	return &llvmcas.CASPutResponse{
		Contents: &llvmcas.CASPutResponse_CasId{
			CasId: casId,
		},
	}, nil
}

func (p *Proxy) Load(ctx context.Context, request *llvmcas.CASLoadRequest) (*llvmcas.CASLoadResponse, error) {
	key := createLLVMCasKey(request.GetCasId())

	p.logger.TDebugf("Load called with request: %s", key)

	var hit bool

	start := time.Now()
	defer func() {
		p.logReadCallStats("Load", key, start, hit)
	}()

	errorHandler := func(err error) *llvmcas.CASLoadResponse {
		if errors.Is(err, kv.ErrCacheNotFound) {
			p.statsCollector.incrementMisses()

			//nolint:exhaustruct
			return &llvmcas.CASLoadResponse{
				Outcome: llvmcas.CASLoadResponse_OBJECT_NOT_FOUND,
			}
		}

		p.logger.TErrorf("Load error: %s", err)

		return &llvmcas.CASLoadResponse{
			Outcome: llvmcas.CASLoadResponse_ERROR,
			Contents: &llvmcas.CASLoadResponse_Error{
				Error: &llvmcas.ResponseError{
					Description: err.Error(),
				},
			},
		}
	}

	buffer := bytes.NewBuffer(nil)
	err := p.kvClient.DownloadStream(ctx, buffer, key)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to download data: %w", err)), nil
	}

	p.statsCollector.addDownloadBytes(int64(buffer.Len()))

	data, err := io.ReadAll(buffer)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to read data: %w", err)), nil
	}

	hit = true
	p.statsCollector.incrementHits()

	return &llvmcas.CASLoadResponse{
		Outcome: llvmcas.CASLoadResponse_SUCCESS,
		Contents: &llvmcas.CASLoadResponse_Data{
			Data: &llvmcas.CASBlob{
				Blob: &llvmcas.CASBytes{
					Contents: &llvmcas.CASBytes_Data{
						Data: data,
					},
				},
			},
		},
	}, nil
}

func (p *Proxy) Save(ctx context.Context, request *llvmcas.CASSaveRequest) (*llvmcas.CASSaveResponse, error) {
	errorHandler := func(err error) *llvmcas.CASSaveResponse {
		p.logger.TErrorf("Save error: %s", err)

		return &llvmcas.CASSaveResponse{
			Contents: &llvmcas.CASSaveResponse_Error{
				Error: &llvmcas.ResponseError{
					Description: err.Error(),
				},
			},
		}
	}

	var key string

	start := time.Now()
	defer func() {
		p.logWriteCallStats("Save", key, start)
	}()

	var reader io.Reader
	var size int64
	if request.GetData().GetBlob().GetFilePath() != "" {
		stat, err := os.Stat(request.GetData().GetBlob().GetFilePath())
		if err != nil {
			return errorHandler(fmt.Errorf("failed to read file %s: %w", request.GetData().GetBlob().GetFilePath(), err)), nil
		}

		size = stat.Size()

		reader, err = os.OpenFile(request.GetData().GetBlob().GetFilePath(), os.O_RDONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", request.GetData().GetBlob().GetFilePath(), err)
		}
		defer func() {
			//nolint:forcetypeassert
			if err := reader.(io.Closer).Close(); err != nil {
				p.logger.TErrorf("Failed to close file reader: %s", err)
			}
		}()
	} else {
		reader = bytes.NewBuffer(request.GetData().GetBlob().GetData())
		size = int64(len(request.GetData().GetBlob().GetData()))
	}

	hasher := hash.NewBlobHasher(digestFunction)
	if _, err := io.Copy(hasher, reader); err != nil {
		return errorHandler(fmt.Errorf("failed to hash data: %w", err)), nil
	}

	casId := &llvmcas.CASDataID{
		Id: hasher.Sum(nil),
	}
	key = createLLVMCasKey(casId)

	p.logger.TDebugf("Save: CAS ID: %s", key)

	// reset the reader
	if request.GetData().GetBlob().GetFilePath() != "" {
		//nolint:forcetypeassert
		if _, err := reader.(io.Seeker).Seek(0, io.SeekStart); err != nil {
			return errorHandler(fmt.Errorf("failed to seek file %s: %w", request.GetData().GetBlob().GetFilePath(), err)), nil
		}
	} else {
		reader = bytes.NewBuffer(request.GetData().GetBlob().GetData())
	}

	p.statsCollector.addUploadBytes(size)

	err := p.kvClient.UploadStreamToBuildCache(ctx, reader, key, size)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to upload data: %w", err)), nil
	}

	return &llvmcas.CASSaveResponse{
		Contents: &llvmcas.CASSaveResponse_CasId{
			CasId: casId,
		},
	}, nil
}

func (p *Proxy) GetValue(ctx context.Context, request *llvmkv.GetValueRequest) (*llvmkv.GetValueResponse, error) {
	key := createLLVMKVKey(request.GetKey())

	var hit bool

	p.logger.TDebugf("GetValue called with key: %s", key)

	start := time.Now()
	defer func() {
		p.logReadCallStats("GetValue", key, start, hit)
	}()

	errorHandler := func(err error) *llvmkv.GetValueResponse {
		if errors.Is(err, kv.ErrCacheNotFound) {
			p.statsCollector.incrementMisses()

			//nolint:exhaustruct
			return &llvmkv.GetValueResponse{
				Outcome: llvmkv.GetValueResponse_KEY_NOT_FOUND,
			}
		}

		p.logger.TErrorf("GetValue error: %s", err)

		return &llvmkv.GetValueResponse{
			Outcome: llvmkv.GetValueResponse_ERROR,
			Contents: &llvmkv.GetValueResponse_Error{
				Error: &llvmkv.ResponseError{
					Description: err.Error(),
				},
			},
		}
	}

	buffer := bytes.NewBuffer(nil)
	err := p.kvClient.DownloadStream(ctx, buffer, key)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to download value: %w", err)), nil
	}

	p.statsCollector.addDownloadBytes(int64(buffer.Len()))

	var entries map[string][]byte
	if err := gob.NewDecoder(buffer).Decode(&entries); err != nil {
		return errorHandler(fmt.Errorf("failed to decode value: %w", err)), nil
	}

	hit = true
	p.statsCollector.incrementHits()

	return &llvmkv.GetValueResponse{
		Outcome: llvmkv.GetValueResponse_SUCCESS,
		Contents: &llvmkv.GetValueResponse_Value{
			Value: &llvmkv.Value{
				Entries: entries,
			},
		},
	}, nil
}

func (p *Proxy) PutValue(ctx context.Context, request *llvmkv.PutValueRequest) (*llvmkv.PutValueResponse, error) {
	key := createLLVMKVKey(request.GetKey())

	p.logger.TDebugf("PutValue called with key: %s", key)

	start := time.Now()
	defer func() {
		p.logWriteCallStats("PutValue", key, start)
	}()

	errorHandler := func(err error) *llvmkv.PutValueResponse {
		p.logger.TErrorf("PutValue error: %s", err)

		return &llvmkv.PutValueResponse{
			Error: &llvmkv.ResponseError{
				Description: err.Error(),
			},
		}
	}

	buffer := bytes.NewBuffer(nil)
	if err := gob.NewEncoder(buffer).Encode(request.GetValue().GetEntries()); err != nil {
		return errorHandler(fmt.Errorf("failed to encode value: %w", err)), nil
	}

	size := int64(buffer.Len())
	p.statsCollector.addUploadBytes(size)

	err := p.kvClient.UploadStreamToBuildCache(ctx, buffer, key, size)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to upload value: %w", err)), nil
	}

	//nolint:exhaustruct
	return &llvmkv.PutValueResponse{}, nil
}

func (p *Proxy) logReadCallStats(method string, key string, start time.Time, hit bool) {
	p.logger.TDebugf("%s with key %s took %s and was a hit: %t",
		method,
		key,
		time.Since(start),
		hit,
	)
}

func (p *Proxy) logWriteCallStats(method string, key string, start time.Time) {
	p.logger.TDebugf("%s with key %s took %s",
		method,
		key,
		time.Since(start),
	)
}

func (p *Proxy) callGetCapabilities(info *grpc.UnaryServerInfo, ctx context.Context) error {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	if p.capabilitiesCalled {
		return nil
	}

	for _, desc := range p.skipGetCapabilitiesCall {
		for _, method := range desc.Methods {
			if info.FullMethod == fmt.Sprintf("/%s/%s", desc.ServiceName, method.MethodName) {
				return nil
			}
		}
	}

	p.capabilitiesCalled = true

	if err := p.kvClient.GetCapabilitiesWithRetry(ctx); err != nil {
		p.logger.TErrorf("GetCapabilities error: %s", err)

		return fmt.Errorf("failed to call GetCapabilities: %w", err)
	}

	return nil
}
