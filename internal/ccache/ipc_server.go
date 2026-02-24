package ccache

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/protocol"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/gofrs/uuid/v5"
)

type IpcServer struct {
	listener           net.Listener
	client             Client
	logger             log.Logger
	ctx                context.Context
	cancel             context.CancelFunc
	loggerFactory      LoggerFactory
	idleTimer          *time.Timer
	sessionState       *sessionState
	ccSemaphore        chan struct{}
	config             Config
	timerMutex         sync.Mutex
	sessionMutex       sync.Mutex
	capabilitiesCalled bool
}

func NewServer(
	ctx context.Context,
	config Config,
	client Client,
	logger log.Logger,
	loggerFactory LoggerFactory,
) (*IpcServer, error) {
	cancellableCtx, cancel := context.WithCancel(ctx)

	return &IpcServer{
		ctx:           cancellableCtx,
		cancel:        cancel,
		config:        config,
		client:        client,
		logger:        logger,
		loggerFactory: loggerFactory,
		sessionState:  newSessionState(),
	}, nil
}

func (s *IpcServer) Run() error {
	listener, err := s.createListener()
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	s.listener = listener
	defer s.listener.Close()

	s.logger.TInfof("Server listening on %s", s.config.IPCEndpoint)
	s.resetIdleTimer()
	go s.acceptLoop()
	<-s.ctx.Done() // wait for context cancellation
	s.logger.TInfof("Server shutting down")
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
				s.logger.TErrorf("Accept error: %v", err)
				continue
			}
		}

		s.logger.TDebugf("[%s] Client connected", conID)
		s.resetIdleTimer()

		go s.handleConnection(conn, conID)
	}
}

func (s *IpcServer) handleConnection(conn net.Conn, conID string) {
	defer conn.Close()

	if err := protocol.WriteGreeting(conn); err != nil {
		s.logger.TErrorf("Failed to send greeting: %v", err)
		return
	}

	err := s.client.GetCapabilitiesWithRetry(s.ctx)
	if err != nil {
		s.logger.TErrorf("Failed to get capabilities: %v", err)
		return
	}

	processor := newRequestProcessor(conn, s.config, s.client, s.logger)
	for {
		result := processor.processRequest()
		s.sessionState.updateWithResult(result)

		if result.Err != nil {
			if result.Err == io.EOF {
				s.logger.TDebugf("[%s] Client disconnected", conID)
			} else {
				s.logger.TErrorf("[%s] Processing error: %v", conID, result.Err)
			}
			return
		}

		if result.Outcome == PROCESS_REQUEST_SHOULD_STOP {
			s.logger.TInfof("Stop requested, shutting down")
			s.cancel()
			return
		}

		s.resetIdleTimer()
	}
}

func (s *IpcServer) resetIdleTimer() {
	if s.config.IdleTimeout == 0 {
		return
	}

	s.timerMutex.Lock()
	defer s.timerMutex.Unlock()

	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}

	s.idleTimer = time.AfterFunc(time.Second*s.config.IdleTimeout, func() {
		s.logger.TInfof("Idle timeout reached, shutting down")
		s.cancel()
	})
}

//-------------------------------
// Session state update from process results
//-------------------------------

type methodAndOutcome struct {
	Method  callMethod
	Outcome processResultOutcome
}

func (s *sessionState) updateWithResult(result processResult) {
	resultMethodAndOutcome := methodAndOutcome{
		Method:  result.CallStats.method,
		Outcome: result.Outcome,
	}

	switch resultMethodAndOutcome {
	case methodAndOutcome{Method: CALL_METHOD_GET, Outcome: PROCESS_REQUEST_OK}:
		s.incrementHits()
		s.addDownloadBytes(result.CallStats.downloadBytes)

	case methodAndOutcome{Method: CALL_METHOD_GET, Outcome: PROCESS_REQUEST_MISS}:
		s.incrementMisses()

	case methodAndOutcome{Method: CALL_METHOD_PUT, Outcome: PROCESS_REQUEST_OK}:
		s.addUploadBytes(result.CallStats.uploadBytes)

	default:
		// We could track other outcomes as well, but for now we focus on the main ones for stats.
	}
}

//-------------------------------
// Session state copy
//-------------------------------

type sessionState struct {
	downloadBytes atomic.Int64
	uploadBytes   atomic.Int64
	hits          atomic.Int64
	misses        atomic.Int64
}

func newSessionState() *sessionState {
	return &sessionState{}
}

func (s *sessionState) addDownloadBytes(n int64) {
	s.downloadBytes.Add(n)
}

func (s *sessionState) addUploadBytes(n int64) {
	s.uploadBytes.Add(n)
}

func (s *sessionState) incrementMisses() {
	s.misses.Add(1)
}

func (s *sessionState) incrementHits() {
	s.hits.Add(1)
}
