package proxy

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/hash"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/slicebuf"
	llvmcas "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/cas"
	llvmkv "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
)

var (
	_ llvmcas.CASDBServiceServer = (*Proxy)(nil)
	_ llvmkv.KeyValueDBServer    = (*Proxy)(nil)
	_ session.SessionServer      = (*Proxy)(nil)
)

//go:generate moq -rm -stub -pkg mocks -out ./mocks/client.go . Client
type Client interface {
	ChangeSession(invocationID string, appSlug string, buildSlug string, stepSlug string)
	SetLogger(logger log.Logger)
	DownloadStream(ctx context.Context, writer io.Writer, key string) error
	UploadStreamToBuildCache(ctx context.Context, reader io.ReadSeeker, key string, size int64) error
	GetCapabilitiesWithRetry(ctx context.Context) error
}

type LoggerFactory func(invocationID string) (log.Logger, error)

type Proxy struct {
	llvmcas.UnimplementedCASDBServiceServer
	llvmkv.UnimplementedKeyValueDBServer
	session.UnimplementedSessionServer

	kvClient                Client
	pushEnabled             bool
	sessionMutex            sync.Mutex
	capabilitiesCalled      bool
	sessionState            *sessionState
	skipGetCapabilitiesCall []grpc.ServiceDesc
	logger                  log.Logger
	loggerFactory           LoggerFactory

	emitter        InvocationEmitter
	currentSession *SessionMeta
	grpcServer     *grpc.Server

	// InactivityTimeout is the idle window after which the current session is
	// slim-emitted. Zero falls back to defaultInactivityTimeout.
	InactivityTimeout time.Duration
	inactivityTimer   *time.Timer
	lastActivity      time.Time

	sidecarStats    *sidecarStatsHandler
	sidecarRegistry *sessionRegistry

	// FlushHook is invoked by the FlushSession RPC. Returns the invocation IDs
	// emitted during the sweep. nil → FlushSession returns an empty list.
	FlushHook func(ctx context.Context) []string
}

// SidecarOptions enables per-connection sidecar writes. Zero / empty disables.
type SidecarOptions struct {
	// Dir is where sessions.Sidecar JSON files are written. Empty disables.
	Dir string
	// Listener is the *PeerListener wrapper the proxy serves on. Required
	// when Dir is set.
	Listener *PeerListener
}

const defaultInactivityTimeout = 5 * time.Minute

func NewProxy(kvClient Client, pushEnabled bool, logger log.Logger, loggerFactory LoggerFactory, emitter InvocationEmitter) *Proxy {
	return NewProxyWithOptions(kvClient, pushEnabled, logger, loggerFactory, emitter, SidecarOptions{})
}

// NewProxyWithOptions is the full constructor; zero-value SidecarOptions
// disables sidecar writes.
func NewProxyWithOptions(
	kvClient Client,
	pushEnabled bool,
	logger log.Logger,
	loggerFactory LoggerFactory,
	emitter InvocationEmitter,
	sidecar SidecarOptions,
) *Proxy {
	//nolint:exhaustruct
	proxy := &Proxy{
		kvClient:      kvClient,
		pushEnabled:   pushEnabled,
		sessionState:  newSessionState(),
		logger:        logger,
		loggerFactory: loggerFactory,
		emitter:       emitter,
		skipGetCapabilitiesCall: []grpc.ServiceDesc{
			session.Session_ServiceDesc, // skip GetCapabilities call for session service methods
		},
	}

	proxy.installSidecar(sidecar)

	serverOpts := []grpc.ServerOption{
		// Unbounded by default: gRPC spawns a goroutine per stream, and the
		// compilation plugin opens hundreds at once. A CPU-starved proxy holds
		// each one far longer, so threads and fds grow with service time. See
		// docs/daemon-latency.md.
		grpc.MaxConcurrentStreams(maxConcurrentStreams),
		grpc.NumStreamWorkers(streamWorkers()),
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

			resp, err := handler(ctx, req)
			if !isSessionServiceMethod(info.FullMethod) {
				proxy.touchSession() //nolint:contextcheck // timer callback fires after RPC ctx is done
			}

			return resp, err
		}),
	}

	if proxy.sidecarStats != nil {
		serverOpts = append(serverOpts, grpc.StatsHandler(proxy.sidecarStats))
	}

	grpcServer := grpc.NewServer(serverOpts...)

	llvmcas.RegisterCASDBServiceServer(grpcServer, proxy)
	llvmkv.RegisterKeyValueDBServer(grpcServer, proxy)
	session.RegisterSessionServer(grpcServer, proxy)

	proxy.grpcServer = grpcServer

	return proxy
}

// installSidecar wires sidecar machinery if opts.Dir is set. A nil Listener
// disables peer-PID tagging.
func (p *Proxy) installSidecar(opts SidecarOptions) {
	if opts.Dir == "" {
		return
	}

	p.sidecarRegistry = newSessionRegistry()
	ancestry := newAncestryCache()
	p.sidecarStats = &sidecarStatsHandler{
		listener:             opts.Listener,
		registry:             p.sidecarRegistry,
		writer:               newSidecarWriter(opts.Dir, p.logger),
		ancestryResolver:     ancestry.resolveAncestry,
		peerAncestryEntries:  ancestry.resolveAncestryEntries,
		hostname:             hostnameForDerivation(),
		wrapperSessionActive: p.wrapperSessionActive,
	}
}

// wrapperSessionActive reports whether the SetSession path currently owns the
// InvocationID. Trampoline/wrapperless derivation defers to the wrapper when true.
func (p *Proxy) wrapperSessionActive() bool {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	return p.currentSession != nil
}

// stateFor returns a recorder that mirrors to the global sessionState and,
// when the ctx carries a per-conn session, to that one too. Keeps sidecar
// accounting in lockstep without changing the global session's semantics.
func (p *Proxy) stateFor(ctx context.Context) *recorder {
	cs := sessionFromContext(ctx)
	if cs == nil {
		return &recorder{global: p.sessionState}
	}

	cs.markActivity()

	return &recorder{global: p.sessionState, conn: cs.state}
}

// Sized off the machine, not the client: the plugin will open as many streams
// as it has compiles, and the proxy's job is to keep its own thread count flat
// while it drains them.
const maxConcurrentStreams = 64

func streamWorkers() uint32 {
	return uint32(max(4, min(runtime.NumCPU()*2, 32))) //nolint:gosec // bounded to 32
}

// Serve delegates to the underlying gRPC server.
func (p *Proxy) Serve(l net.Listener) error {
	//nolint:wrapcheck
	return p.grpcServer.Serve(l)
}

// GracefulStop stops the underlying gRPC server after in-flight RPCs finish.
// After the server returns, any outstanding per-connection sidecars are
// flushed — otherwise a shutdown mid-build loses the still-open sessions.
func (p *Proxy) GracefulStop() {
	p.grpcServer.GracefulStop()
	p.flushOutstandingSidecars()
}

func (p *Proxy) flushOutstandingSidecars() {
	if p.sidecarRegistry == nil || p.sidecarStats == nil {
		return
	}

	p.sidecarRegistry.flushAll(p.sidecarStats.flushSession)
}

// FlushCurrentSession emits the currently-open session (if any) via the configured emitter.
// Caller is responsible for invoking this on shutdown — SetSession() already emits the
// previous session before starting a new one.
func (p *Proxy) FlushCurrentSession(ctx context.Context) {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	p.emitCurrentSessionLocked(ctx)
}

// emitCurrentSessionLocked snapshots + emits under the caller-held sessionMutex.
func (p *Proxy) emitCurrentSessionLocked(ctx context.Context) {
	if p.emitter == nil || p.currentSession == nil {
		return
	}

	if p.inactivityTimer != nil {
		p.inactivityTimer.Stop()
		p.inactivityTimer = nil
	}

	meta := *p.currentSession
	meta.EndTime = p.lastActivity
	stats := p.sessionState.getStats().toPublic()

	p.emitter.EmitSlim(ctx, meta, stats)

	p.currentSession = nil
}

func (p *Proxy) inactivityDuration() time.Duration {
	if p.InactivityTimeout > 0 {
		return p.InactivityTimeout
	}

	return defaultInactivityTimeout
}

// isSessionServiceMethod reports whether the fully-qualified gRPC method belongs
// to the session control plane (SetSession / RegisterInvocationRelation …).
// Control-plane calls are not cache activity: bumping lastActivity for them
// would overwrite the explicit reset SetSession does at line 232 and lose the
// "fresh session, no RPCs yet" signal that meta.EndTime relies on.
func isSessionServiceMethod(fullMethod string) bool {
	return strings.HasPrefix(fullMethod, "/"+session.Session_ServiceDesc.ServiceName+"/")
}

// touchSession records RPC activity on the current session and arms the
// inactivity timer on the first touch.
func (p *Proxy) touchSession() {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	if p.currentSession == nil {
		return
	}

	p.lastActivity = time.Now()

	if p.inactivityTimer == nil {
		target := p.currentSession
		p.inactivityTimer = time.AfterFunc(p.inactivityDuration(), func() {
			p.onInactivity(target)
		})
	}
}

// onInactivity fires when the timer elapses. Re-schedules itself if touchSession
// bumped lastActivity in the meantime; emits otherwise.
func (p *Proxy) onInactivity(target *SessionMeta) {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	if p.currentSession != target {
		return
	}

	elapsed := time.Since(p.lastActivity)
	if remaining := p.inactivityDuration() - elapsed; remaining > 0 {
		p.inactivityTimer = time.AfterFunc(remaining, func() {
			p.onInactivity(target)
		})

		return
	}

	p.emitCurrentSessionLocked(context.Background())
}

// EndSession flushes the currently-open session if its InvocationID matches
// req.GetInvocationId(). Mismatched or absent session is a silent no-op — the
// wrapper calls this after xcodebuild exits and the ordering vs SetSession
// swap is not guaranteed.
func (p *Proxy) EndSession(ctx context.Context, req *session.EndSessionRequest) (*emptypb.Empty, error) {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	if p.currentSession == nil {
		p.logger.TDebugf("EndSession no-op for %s: no active session", req.GetInvocationId())

		return &emptypb.Empty{}, nil
	}

	if p.currentSession.InvocationID != req.GetInvocationId() {
		p.logger.TDebugf("EndSession no-op: got %s, active %s", req.GetInvocationId(), p.currentSession.InvocationID)

		return &emptypb.Empty{}, nil
	}

	if endMs := req.GetEndTimeUnixMs(); endMs > 0 {
		p.lastActivity = time.UnixMilli(endMs)
	}

	p.emitCurrentSessionLocked(ctx)

	return &emptypb.Empty{}, nil
}

func (p *Proxy) SetSession(ctx context.Context, request *session.SetSessionRequest) (*emptypb.Empty, error) {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	p.emitCurrentSessionLocked(ctx)

	p.capabilitiesCalled = false

	p.kvClient.ChangeSession(request.GetInvocationId(), request.GetAppSlug(), request.GetBuildSlug(), request.GetStepSlug())

	p.sessionState = newSessionState()
	p.currentSession = &SessionMeta{
		InvocationID: request.GetInvocationId(),
		AppSlug:      request.GetAppSlug(),
		BuildSlug:    request.GetBuildSlug(),
		StepSlug:     request.GetStepSlug(),
		StartTime:    time.Now(),
	}
	p.lastActivity = time.Time{}
	p.inactivityTimer = nil

	logger, err := p.loggerFactory(request.GetInvocationId())
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}
	p.logger = logger
	p.kvClient.SetLogger(p.logger)

	p.logger.TInfof("SetSession called with invocation ID: %s, app slug: %s, build slug: %s, step slug: %s",
		request.GetInvocationId(),
		request.GetAppSlug(),
		request.GetBuildSlug(),
		request.GetStepSlug(),
	)

	return &emptypb.Empty{}, nil
}

// FlushSession runs the registered flush hook synchronously and returns the
// invocation IDs the hook reports as emitted. nil hook → empty list.
func (p *Proxy) FlushSession(ctx context.Context, _ *emptypb.Empty) (*session.FlushSessionResponse, error) {
	if p.FlushHook == nil {
		return &session.FlushSessionResponse{}, nil
	}

	ids := p.FlushHook(ctx)

	return &session.FlushSessionResponse{EmittedInvocationIds: ids}, nil
}

func (p *Proxy) GetSessionStats(_ context.Context, _ *emptypb.Empty) (*session.GetSessionStatsResponse, error) {
	collectedStats := p.sessionState.getStats()

	return &session.GetSessionStatsResponse{
		UploadedBytes:   collectedStats.uploadBytes,
		DownloadedBytes: collectedStats.downloadBytes,
		Hits:            collectedStats.hits,
		Misses:          collectedStats.misses,
		Uploads:         collectedStats.uploads,
		KvHits:          collectedStats.kvHits,
		KvMisses:        collectedStats.kvMisses,
		KvUploadedBytes: collectedStats.kvUploadBytes,
		Errors:          collectedStats.errors,
		FirstError:      collectedStats.firstError,
		CacheBlobStats:  blobstats.ToProto(collectedStats.blobStats),
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

	rec := p.stateFor(ctx)

	errorHandler := func(err error) *llvmcas.CASGetResponse {
		if errors.Is(err, kv.ErrCacheNotFound) {
			rec.recordMiss(opGet)

			//nolint:exhaustruct
			return &llvmcas.CASGetResponse{
				Outcome: llvmcas.CASGetResponse_OBJECT_NOT_FOUND,
			}
		}

		rec.recordError(opGet, err)
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
	transferStart := time.Now()
	err := p.kvClient.DownloadStream(ctx, buffer, key)
	transferElapsed := time.Since(transferStart)
	if err != nil {
		return errorHandler(fmt.Errorf("%s: failed to download data: %w", key, err)), nil
	}

	rec.saveKeyOnce(key)
	size := int64(buffer.Len())

	data := blob{} //nolint:exhaustruct
	if err := gob.NewDecoder(buffer).Decode(&data); err != nil {
		return errorHandler(fmt.Errorf("%s: failed to decode data: %w", key, err)), nil
	}
	references := make([]*llvmcas.CASDataID, 0, len(data.References))
	for _, ref := range data.References {
		references = append(references, &llvmcas.CASDataID{
			Id: ref,
		})
	}

	hit = true
	rec.recordDownload(opGet, size, transferElapsed)

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

	var key string

	rec := p.stateFor(ctx)

	errorHandler := func(err error) *llvmcas.CASPutResponse {
		rec.markKeyUnsaved(key)

		rec.recordError(opPut, err)
		p.logger.TErrorf("Put error: %s", err)

		return &llvmcas.CASPutResponse{
			Contents: &llvmcas.CASPutResponse_Error{
				Error: &llvmcas.ResponseError{
					Description: err.Error(),
				},
			},
		}
	}

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
	buffer := slicebuf.NewBuffer()

	if err := gob.NewEncoder(io.MultiWriter(hasher, buffer)).Encode(rawData); err != nil {
		return errorHandler(fmt.Errorf("failed to encode data: %w", err)), nil
	}

	casId := &llvmcas.CASDataID{
		Id: hasher.Sum(nil),
	}
	key = createLLVMCasKey(casId)

	p.logger.TDebugf("Put: CAS ID: %s", key)

	if rec.saveKeyOnce(key) {
		p.logger.TDebugf("Put: CAS ID already saved in this session: %s", key)
		rec.recordSkippedAlreadySaved(opPut)

		return &llvmcas.CASPutResponse{
			Contents: &llvmcas.CASPutResponse_CasId{
				CasId: casId,
			},
		}, nil
	}

	if !p.pushEnabled {
		p.logger.TDebugf("Put: Push disabled, not uploading CAS ID: %s", key)

		return &llvmcas.CASPutResponse{
			Contents: &llvmcas.CASPutResponse_CasId{
				CasId: casId,
			},
		}, nil
	}

	size := int64(buffer.Len())

	transferStart := time.Now()
	err := p.kvClient.UploadStreamToBuildCache(ctx, buffer, key, size)
	transferElapsed := time.Since(transferStart)
	if err != nil {
		return errorHandler(fmt.Errorf("failed to upload data: %w", err)), nil
	}

	rec.recordUpload(opPut, size, transferElapsed)

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

	rec := p.stateFor(ctx)

	errorHandler := func(err error) *llvmcas.CASLoadResponse {
		if errors.Is(err, kv.ErrCacheNotFound) {
			rec.recordMiss(opLoad)

			//nolint:exhaustruct
			return &llvmcas.CASLoadResponse{
				Outcome: llvmcas.CASLoadResponse_OBJECT_NOT_FOUND,
			}
		}

		rec.recordError(opLoad, err)
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
	transferStart := time.Now()
	err := p.kvClient.DownloadStream(ctx, buffer, key)
	transferElapsed := time.Since(transferStart)
	if err != nil {
		return errorHandler(fmt.Errorf("%s: failed to download data: %w", key, err)), nil
	}

	rec.saveKeyOnce(key)
	size := int64(buffer.Len())

	data, err := io.ReadAll(buffer)
	if err != nil {
		return errorHandler(fmt.Errorf("%s: failed to read data: %w", key, err)), nil
	}

	hit = true
	rec.recordDownload(opLoad, size, transferElapsed)

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
	var key string

	rec := p.stateFor(ctx)

	errorHandler := func(err error) *llvmcas.CASSaveResponse {
		rec.markKeyUnsaved(key)

		rec.recordError(opSave, err)
		p.logger.TErrorf("Save error: %s", err)

		return &llvmcas.CASSaveResponse{
			Contents: &llvmcas.CASSaveResponse_Error{
				Error: &llvmcas.ResponseError{
					Description: err.Error(),
				},
			},
		}
	}

	start := time.Now()
	defer func() {
		p.logWriteCallStats("Save", key, start)
	}()

	var reader io.ReadSeeker
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
		reader = slicebuf.NewBufferWithData(request.GetData().GetBlob().GetData())
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

	if rec.saveKeyOnce(key) {
		p.logger.TDebugf("Save: CAS ID already saved in this session: %s", key)
		rec.recordSkippedAlreadySaved(opSave)

		return &llvmcas.CASSaveResponse{
			Contents: &llvmcas.CASSaveResponse_CasId{
				CasId: casId,
			},
		}, nil
	}

	if !p.pushEnabled {
		p.logger.TDebugf("Save: Push disabled, not uploading CAS ID: %s", key)

		return &llvmcas.CASSaveResponse{
			Contents: &llvmcas.CASSaveResponse_CasId{
				CasId: casId,
			},
		}, nil
	}

	// reset the reader
	if request.GetData().GetBlob().GetFilePath() != "" {
		//nolint:forcetypeassert
		if _, err := reader.(io.Seeker).Seek(0, io.SeekStart); err != nil {
			return errorHandler(fmt.Errorf("failed to seek file %s: %w", request.GetData().GetBlob().GetFilePath(), err)), nil
		}
	} else {
		reader = slicebuf.NewBufferWithData(request.GetData().GetBlob().GetData())
	}

	transferStart := time.Now()
	err := p.kvClient.UploadStreamToBuildCache(ctx, reader, key, size)
	transferElapsed := time.Since(transferStart)
	if err != nil {
		return errorHandler(fmt.Errorf("%s: failed to upload data: %w", key, err)), nil
	}

	rec.recordUpload(opSave, size, transferElapsed)

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

	rec := p.stateFor(ctx)

	errorHandler := func(err error) *llvmkv.GetValueResponse {
		if errors.Is(err, kv.ErrCacheNotFound) {
			rec.recordMiss(opGetValue)

			//nolint:exhaustruct
			return &llvmkv.GetValueResponse{
				Outcome: llvmkv.GetValueResponse_KEY_NOT_FOUND,
			}
		}

		rec.recordError(opGetValue, err)
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
	transferStart := time.Now()
	err := p.kvClient.DownloadStream(ctx, buffer, key)
	transferElapsed := time.Since(transferStart)
	if err != nil {
		return errorHandler(fmt.Errorf("%s: failed to download value: %w", key, err)), nil
	}

	size := int64(buffer.Len())

	var entries map[string][]byte
	if err := gob.NewDecoder(buffer).Decode(&entries); err != nil {
		return errorHandler(fmt.Errorf("%s: failed to decode value: %w", key, err)), nil
	}

	hit = true
	rec.recordDownload(opGetValue, size, transferElapsed)

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

	rec := p.stateFor(ctx)

	errorHandler := func(err error) *llvmkv.PutValueResponse {
		rec.recordError(opPutValue, err)
		p.logger.TErrorf("PutValue error: %s", err)

		return &llvmkv.PutValueResponse{
			Error: &llvmkv.ResponseError{
				Description: err.Error(),
			},
		}
	}

	if !p.pushEnabled {
		p.logger.TDebugf("PutValue: Push disabled, not uploading key: %s", key)

		return &llvmkv.PutValueResponse{}, nil
	}

	buffer := slicebuf.NewBuffer()
	if err := gob.NewEncoder(buffer).Encode(request.GetValue().GetEntries()); err != nil {
		return errorHandler(fmt.Errorf("failed to encode value: %w", err)), nil
	}

	size := int64(buffer.Len())

	transferStart := time.Now()
	err := p.kvClient.UploadStreamToBuildCache(ctx, buffer, key, size)
	transferElapsed := time.Since(transferStart)
	if err != nil {
		return errorHandler(fmt.Errorf("%s: failed to upload value: %w", key, err)), nil
	}

	rec.recordUpload(opPutValue, size, transferElapsed)

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
