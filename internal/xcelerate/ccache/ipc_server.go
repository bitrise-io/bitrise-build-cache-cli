package ccache

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	cfg "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/ccache/protocol"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/proxy"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/session"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/gofrs/uuid/v5"
	"google.golang.org/protobuf/types/known/emptypb"
)

type IpcServer struct {
	listener           net.Listener
	client             proxy.Client
	logger             log.Logger
	ctx                context.Context
	cancel             context.CancelFunc
	loggerFactory      proxy.LoggerFactory
	idleTimer          *time.Timer
	sessionState       *sessionState
	ccSemaphore        chan struct{}
	config             cfg.Config
	timerMutex         sync.Mutex
	sessionMutex       sync.Mutex
	capabilitiesCalled bool
}

func NewServer(
	ctx context.Context,
	config cfg.Config,
	client proxy.Client,
	logger log.Logger,
	loggerFactory proxy.LoggerFactory,
) (*IpcServer, error) {
	cancellableCtx, cancel := context.WithCancel(ctx)

	return &IpcServer{
		ctx:           cancellableCtx,
		cancel:        cancel,
		config:        config,
		client:        client,
		logger:        logger,
		loggerFactory: loggerFactory,
	}, nil
}

func (s *IpcServer) Run() error {
	listener, err := s.createListener()
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	s.listener = listener
	defer s.listener.Close()

	s.logger.Infof("Server listening on %s", s.config.CCacheConfig.IPCEndpoint)
	s.resetIdleTimer()
	go s.acceptLoop()
	<-s.ctx.Done() // wait for context cancellation
	s.logger.Infof("Server shutting down")
	s.listener.Close()

	return nil
}

func (s *IpcServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		conID := uuid.Must(uuid.NewV4()).String()[:8]
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				s.logger.Infof("Accept error: %v", err)
				continue
			}
		}

		s.logger.Infof("[%s] Client connected", conID)
		s.resetIdleTimer()

		go s.handleConnection(conn, conID)
	}
}

func (s *IpcServer) handleConnection(conn net.Conn, conID string) {
	defer conn.Close()

	if err := protocol.WriteGreeting(conn); err != nil {
		s.logger.Errorf("Failed to send greeting: %v", err)
		return
	}

	err := s.client.GetCapabilitiesWithRetry(s.ctx)
	if err != nil {
		s.logger.Errorf("Failed to get capabilities: %v", err)
		return
	}

	processor := newRequestProcessor(conn, s.config, s.client, s.logger)
	for {
		result := processor.processRequest()
		// s.updateSessionStateWithResult(conID, result)

		// TODO session state modification
		if result.Err != nil {
			if result.Err == io.EOF {
				s.logger.Infof("[%s] Client disconnected, key: %s", conID, result.CallStats.key)
			} else {
				s.logger.Errorf("[%s] Processing error: %v", conID, result.Err)
				s.logger.Errorf("[%s] Error in client, keys: %v", conID, s.keysOfClient(result.CallStats.key))
			}
			return
		}

		if result.Outcome == PROCESS_REQUEST_SHOULD_STOP {
			s.logger.Infof("Stop requested, shutting down")
			s.cancel()
			return
		}

		s.resetIdleTimer()
	}
}

func (s *IpcServer) resetIdleTimer() {
	if s.config.CCacheConfig.IdleTimeout == 0 {
		return
	}

	s.timerMutex.Lock()
	defer s.timerMutex.Unlock()

	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}

	s.idleTimer = time.AfterFunc(time.Second*s.config.CCacheConfig.IdleTimeout, func() {
		s.logger.Infof("Idle timeout reached, shutting down")
		s.cancel()
	})
}

func (s *IpcServer) setSession(_ context.Context, request *session.SetSessionRequest) (*emptypb.Empty, error) {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()

	s.capabilitiesCalled = false

	s.client.ChangeSession(request.GetInvocationId(), request.GetAppSlug(), request.GetBuildSlug(), request.GetStepSlug())

	s.sessionState = newSessionState()

	logger, err := s.loggerFactory(request.GetInvocationId())
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}
	s.logger = logger
	s.client.SetLogger(s.logger)

	s.logger.TInfof("SetSession called with invocation ID: %s, app slug: %s, build slug: %s, step slug: %s",
		request.GetInvocationId(),
		request.GetAppSlug(),
		request.GetBuildSlug(),
		request.GetStepSlug(),
	)

	return &emptypb.Empty{}, nil
}

func (s *IpcServer) getSessionStats(_ context.Context, _ *emptypb.Empty) (*session.GetSessionStatsResponse, error) {
	collectedStats := s.sessionState.getStats()

	return &session.GetSessionStatsResponse{
		UploadedBytes:   collectedStats.uploadBytes,
		DownloadedBytes: collectedStats.downloadBytes,
		Hits:            collectedStats.hits,
		Misses:          collectedStats.misses,
	}, nil
}

//-------------------------------
// Session state update from process results
//-------------------------------

func (s *IpcServer) updateSessionStateWithResult(clientID string, result processResult) {
	switch result.Outcome {
	case PROCESS_REQUEST_OK:
		if result.CallStats.method == "Get" {
			// s.sessionState.incrementHits()
			s.sessionState.saveKeyOnce(clientID, result.CallStats.key)
		}

	case PROCESS_REQUEST_MISS:
		s.sessionState.incrementMisses()

	case PROCESS_REQUEST_ERROR:
		// no-op for now, could be extended to track errors
	}

	if result.CallStats.uploadBytes > 0 {
		s.sessionState.addUploadBytes(result.CallStats.uploadBytes)
	}

	if result.CallStats.downloadBytes > 0 {
		s.sessionState.addDownloadBytes(result.CallStats.downloadBytes)
	}
}

func (s *IpcServer) keysOfClient(key string) map[string]bool {
	// keys, _ := s.sessionState.clients.LoadOrStore(key, map[string]bool{})
	// return keys.(map[string]bool)
	return map[string]bool{} // for now, to avoid concurrency issues with the clients map
}

//-------------------------------
// Session state copy
//-------------------------------

type sessionState struct {
	savedKeys     sync.Map
	clients       sync.Map
	downloadBytes atomic.Int64
	uploadBytes   atomic.Int64
	hits          atomic.Int64
	misses        atomic.Int64
}

type stats struct {
	downloadBytes int64
	uploadBytes   int64
	misses        int64
	hits          int64
}

func newSessionState() *sessionState {
	return &sessionState{
		savedKeys: sync.Map{},
		clients:   sync.Map{},
	}
}

func (s *sessionState) addDownloadBytes(n int64) {
	s.downloadBytes.Add(n)
}

func (s *sessionState) addUploadBytes(n int64) {
	s.uploadBytes.Add(n)
}

func (s *sessionState) getStats() stats {
	return stats{
		downloadBytes: s.downloadBytes.Load(),
		uploadBytes:   s.uploadBytes.Load(),
		hits:          s.hits.Load(),
		misses:        s.misses.Load(),
	}
}

func (s *sessionState) incrementMisses() {
	s.misses.Add(1)
}

func (s *sessionState) incrementHits() {
	s.hits.Add(1)
}

func (s *sessionState) saveKeyOnce(clientID string, key string) bool {
	_, loaded := s.savedKeys.LoadOrStore(key, struct{}{})

	keys, _ := s.clients.LoadOrStore(clientID, map[string]bool{})
	keyMap := keys.(map[string]bool)
	keyMap[key] = true
	s.clients.Store(clientID, keyMap)

	return loaded
}

func (s *sessionState) markKeyUnsaved(key string) {
	s.savedKeys.Delete(key)
}
