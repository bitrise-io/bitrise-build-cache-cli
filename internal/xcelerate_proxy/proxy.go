package xcelerate_proxy

import (
	"bytes"
	"cmp"
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/grpcutil"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/hasher"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/llvm"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/proto/build/bazel/remote/execution/v2"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/kv_storage"
	llvmcas "github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/cas"
	llvmkv "github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/session"
)

//go:generate moq -rm -stub -pkg mock -out ./mock/kv_storage.go ./../../proto/kv_storage KVStorageClient
//go:generate moq -rm -stub -pkg mock -out ./mock/remote_execution.go ./../../proto/build/bazel/remote/execution/v2 CapabilitiesClient

const (
	toolName                   = "xcelerate"
	headerRequestMetadataKey   = "build.bazel.remote.execution.v2.requestmetadata-bin"
	headerBuildToolMetadataKey = "x-flare-buildtool"
	headerAppIdMetadataKey     = "x-app-id"
	headerOrgIdMetadataKey     = "x-org-id"
	headerBuildIdMetadataKey   = "x-flare-build-id"
	headerStepIdMetadataKey    = "x-flare-step-id"
)

var (
	_ llvmcas.CASDBServiceServer = (*Proxy)(nil)
	_ llvmkv.KeyValueDBServer    = (*Proxy)(nil)
	_ session.SessionServer      = (*Proxy)(nil)
)

type sessionParams struct {
	InvocationID    string
	AppSlug         string
	BuildSlug       string
	StepExecutionID string
}

type Proxy struct {
	llvmcas.UnimplementedCASDBServiceServer
	llvmkv.UnimplementedKeyValueDBServer
	session.UnimplementedSessionServer

	cacheClient        kv_storage.KVStorageClient
	capabilitiesClient remoteexecution.CapabilitiesClient

	token   string
	orgID   string
	session sessionParams

	invocationRMD string
	sessionMutex  sync.Mutex

	logger log.Logger
}

func NewProxy(
	cacheClient kv_storage.KVStorageClient,
	capabilitiesClient remoteexecution.CapabilitiesClient,
	token string,
	appSlug string,
	orgID string,
	invocationID string,
	buildSlug string,
	stepExecutionID string,
	logger log.Logger,
) *grpc.Server {
	//nolint:exhaustruct
	p := &Proxy{
		cacheClient:        cacheClient,
		capabilitiesClient: capabilitiesClient,
		token:              token,
		orgID:              orgID,
		logger:             logger,
	}
	p.setSession(invocationID, appSlug, buildSlug, stepExecutionID)

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(func(
			ctx context.Context,
			req any,
			info *grpc.UnaryServerInfo,
			handler grpc.UnaryHandler,
		) (any, error) {
			logger.TDebugf(info.FullMethod)

			var err error
			ctx, err = p.newContextWithAuth(ctx)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create context: %v", err)
			}

			return handler(ctx, req)
		}),
	)

	llvmcas.RegisterCASDBServiceServer(grpcServer, p)
	llvmkv.RegisterKeyValueDBServer(grpcServer, p)
	session.RegisterSessionServer(grpcServer, p)

	return grpcServer
}

func (p *Proxy) SetSession(_ context.Context, request *session.SetSessionRequest) (*emptypb.Empty, error) {
	p.setSession(request.GetInvocationId(), request.GetAppSlug(), request.GetBuildSlug(), request.GetStepSlug())

	return &emptypb.Empty{}, nil
}

func (p *Proxy) Get(ctx context.Context, request *llvmcas.CASGetRequest) (*llvmcas.CASGetResponse, error) {
	key := llvm.CreateLLVMCasKey(request.GetCasId())

	p.logger.TInfof("Get called with request: %s", key)

	errorHandler := func(err error) *llvmcas.CASGetResponse {
		if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
			p.logger.TInfof("miss")

			//nolint:exhaustruct
			return &llvmcas.CASGetResponse{
				Outcome: llvmcas.CASGetResponse_OBJECT_NOT_FOUND,
			}
		}

		return &llvmcas.CASGetResponse{
			Outcome: llvmcas.CASGetResponse_ERROR,
			Contents: &llvmcas.CASGetResponse_Error{
				Error: &llvmcas.ResponseError{
					Description: err.Error(),
				},
			},
		}
	}

	resp, err := p.cacheClient.Get(ctx, &bytestream.ReadRequest{
		ResourceName: key,
		ReadOffset:   0,
		ReadLimit:    0,
	})
	if err != nil {
		return errorHandler(fmt.Errorf("failed to get data: %w", err)), nil
	}

	reader := grpcutil.NewReader(resp)
	defer func() {
		if err := reader.Close(); err != nil {
			p.logger.TErrorf("Failed to close reader: %s", err)
		}
	}()

	data := llvm.LLVMBlob{} //nolint:exhaustruct
	if err := gob.NewDecoder(reader).Decode(&data); err != nil {
		return errorHandler(fmt.Errorf("failed to decode data: %w", err)), nil
	}
	references := make([]*llvmcas.CASDataID, 0, len(data.References))
	for _, ref := range data.References {
		references = append(references, &llvmcas.CASDataID{
			Id: ref,
		})
	}

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
	p.logger.TInfof("Put called with references: %s", request.GetData().GetReferences())

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

	var data []byte
	if request.GetData().GetBlob().GetFilePath() != "" {
		var err error
		data, err = os.ReadFile(request.GetData().GetBlob().GetFilePath())
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", request.GetData().GetBlob().GetFilePath(), err)
		}
	} else {
		data = request.GetData().GetBlob().GetData()
	}

	rawData := &llvm.LLVMBlob{
		Data:       data,
		References: make([][]byte, 0, len(request.GetData().GetReferences())),
	}

	for _, ref := range request.GetData().GetReferences() {
		rawData.References = append(rawData.References, ref.GetId())
	}

	hasher := hasher.NewBlobHasher(llvm.LLVMHash)
	buffer := bytes.NewBuffer(nil)

	if err := gob.NewEncoder(io.MultiWriter(hasher, buffer)).Encode(rawData); err != nil {
		return errorHandler(fmt.Errorf("failed to encode data: %w", err)), nil
	}

	casId := &llvmcas.CASDataID{
		Id: hasher.Sum(nil),
	}
	key := llvm.CreateLLVMCasKey(casId)

	p.logger.TInfof("Put: CAS ID: %s", key)

	stream, err := p.cacheClient.Put(ctx)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to create writer: %w", err)), nil
	}

	writer := grpcutil.NewClientWriter(stream, key, 0)

	if _, err := io.Copy(writer, buffer); err != nil {
		writer.Abort()

		return errorHandler(fmt.Errorf("failed to encode data: %w", err)), nil
	}

	if err := writer.Close(); err != nil {
		return errorHandler(fmt.Errorf("failed to close writer: %w", err)), nil
	}

	return &llvmcas.CASPutResponse{
		Contents: &llvmcas.CASPutResponse_CasId{
			CasId: casId,
		},
	}, nil
}

func (p *Proxy) Load(ctx context.Context, request *llvmcas.CASLoadRequest) (*llvmcas.CASLoadResponse, error) {
	key := llvm.CreateLLVMCasKey(request.GetCasId())

	p.logger.TInfof("Load called with request: %s", key)

	errorHandler := func(err error) *llvmcas.CASLoadResponse {
		if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
			p.logger.TInfof("miss")

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

	resp, err := p.cacheClient.Get(ctx, &bytestream.ReadRequest{
		ResourceName: key,
		ReadOffset:   0,
		ReadLimit:    0,
	})
	if err != nil {
		return errorHandler(fmt.Errorf("failed to get data: %w", err)), nil
	}

	reader := grpcutil.NewReader(resp)
	defer func() {
		if err := reader.Close(); err != nil {
			p.logger.TErrorf("Failed to close reader: %s", err)
		}
	}()

	data, err := io.ReadAll(reader)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to read data: %w", err)), nil
	}

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

	var reader io.Reader
	if request.GetData().GetBlob().GetFilePath() != "" {
		var err error
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
	}

	hasher := hasher.NewBlobHasher(llvm.LLVMHash)

	if _, err := io.Copy(hasher, reader); err != nil {
		return errorHandler(fmt.Errorf("failed to hash data: %w", err)), nil
	}

	casId := &llvmcas.CASDataID{
		Id: hasher.Sum(nil),
	}
	key := llvm.CreateLLVMCasKey(casId)

	p.logger.TInfof("Save: CAS ID: %s", key)

	stream, err := p.cacheClient.Put(ctx)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to create writer: %w", err)), nil
	}

	writer := grpcutil.NewClientWriter(stream, key, 0)

	// reset the reader
	if request.GetData().GetBlob().GetFilePath() != "" {
		//nolint:forcetypeassert
		if _, err := reader.(io.Seeker).Seek(0, io.SeekStart); err != nil {
			writer.Abort()

			return errorHandler(fmt.Errorf("failed to seek file %s: %w", request.GetData().GetBlob().GetFilePath(), err)), nil
		}
	} else {
		reader = bytes.NewBuffer(request.GetData().GetBlob().GetData())
	}

	if n, err := io.Copy(writer, reader); err != nil {
		writer.Abort()

		return errorHandler(fmt.Errorf("failed to copy file %s: %w", request.GetData().GetBlob().GetFilePath(), err)), nil
	} else {
		p.logger.TInfof("Saved data size: %d", n)
	}

	if err := writer.Close(); err != nil {
		return errorHandler(fmt.Errorf("failed to close writer: %w", err)), nil
	}

	return &llvmcas.CASSaveResponse{
		Contents: &llvmcas.CASSaveResponse_CasId{
			CasId: casId,
		},
	}, nil
}

func (p *Proxy) GetValue(ctx context.Context, request *llvmkv.GetValueRequest) (*llvmkv.GetValueResponse, error) {
	key := llvm.CreateLLVMKVKey(request.GetKey())

	p.logger.TInfof("GetValue called with key: %s", key)

	errorHandler := func(err error) *llvmkv.GetValueResponse {
		if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
			p.logger.TInfof("miss")
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

	stream, err := p.cacheClient.Get(ctx, &bytestream.ReadRequest{
		ResourceName: key,
	})
	if err != nil {
		return errorHandler(fmt.Errorf("failed to create stream: %w", err)), nil
	}

	reader := grpcutil.NewReader(stream)
	defer func() {
		if err := reader.Close(); err != nil {
			p.logger.TErrorf("Failed to close reader: %s", err)
		}
	}()

	var entries map[string][]byte
	if err := gob.NewDecoder(reader).Decode(&entries); err != nil {
		return errorHandler(fmt.Errorf("failed to decode value: %w", err)), nil
	}

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
	key := llvm.CreateLLVMKVKey(request.GetKey())

	p.logger.TInfof("PutValue called with key: %s", key)

	errorHandler := func(err error) *llvmkv.PutValueResponse {
		p.logger.TErrorf("PutValue error: %s", err)

		return &llvmkv.PutValueResponse{
			Error: &llvmkv.ResponseError{
				Description: err.Error(),
			},
		}
	}

	stream, err := p.cacheClient.Put(ctx)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to create strem: %w", err)), nil
	}

	writer := grpcutil.NewClientWriter(stream, key, 0)
	if err := gob.NewEncoder(writer).Encode(request.GetValue().GetEntries()); err != nil {
		writer.Abort()

		return errorHandler(fmt.Errorf("failed to encode value: %w", err)), nil
	}

	if err := writer.Close(); err != nil {
		return errorHandler(fmt.Errorf("failed to close writer: %w", err)), nil
	}

	//nolint:exhaustruct
	return &llvmkv.PutValueResponse{}, nil
}

func (p *Proxy) newContextWithAuth(ctx context.Context) (context.Context, error) {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	return p.addSessionHeaders(
		p.addNonEmpty(ctx,
			map[string]string{
				headerBuildToolMetadataKey: toolName,
				"authorization":            "bearer " + p.token,
				headerOrgIdMetadataKey:     p.orgID,
			},
		),
	)
}

func (p *Proxy) addSessionHeaders(ctx context.Context) (context.Context, error) {
	var callGetCapabilities bool
	if p.invocationRMD == "" {
		callGetCapabilities = true

		rmd, err := proto.Marshal(&remoteexecution.RequestMetadata{
			ToolInvocationId: p.session.InvocationID,
			ToolDetails: &remoteexecution.ToolDetails{
				ToolName: toolName,
			},
		})
		if err != nil {
			return ctx, fmt.Errorf("failed to marshal RequestMetadata: %w", err)
		}
		p.invocationRMD = string(rmd)
	}

	ctx = p.addNonEmpty(ctx, map[string]string{
		headerRequestMetadataKey: p.invocationRMD,
		headerAppIdMetadataKey:   p.session.AppSlug,
		headerBuildIdMetadataKey: p.session.BuildSlug,
		headerStepIdMetadataKey:  p.session.StepExecutionID,
	})

	if callGetCapabilities {
		if err := p.callGetCapabilities(ctx); err != nil {
			return ctx, err
		}
	}

	return ctx, nil
}

func (p *Proxy) addNonEmpty(ctx context.Context, headers map[string]string) context.Context {
	for k, v := range headers {
		if v == "" {
			continue
		}
		ctx = metadata.AppendToOutgoingContext(ctx, k, v) //nolint:fatcontext
	}

	return ctx
}

func (p *Proxy) setSession(invocationID string, appSlug string, buildSlug string, stepSlug string) {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	p.invocationRMD = ""
	p.session.InvocationID = cmp.Or(invocationID, uuid.New().String()) // never leave it empty
	p.session.AppSlug = appSlug
	p.session.BuildSlug = buildSlug
	p.session.StepExecutionID = stepSlug

	p.logger.TInfof("session changed: %+v", p.session)
}

func (p *Proxy) callGetCapabilities(ctx context.Context) error {
	_, err := p.capabilitiesClient.GetCapabilities(ctx, &remoteexecution.GetCapabilitiesRequest{})
	if err != nil {
		return fmt.Errorf("failed to call GetCapabilities: %w", err)
	}

	return nil
}
