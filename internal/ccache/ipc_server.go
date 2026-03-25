package ccache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/gofrs/uuid/v5"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/protocol"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

type IpcServer struct {
	listener           net.Listener
	client             Client
	logger             log.Logger
	loggerFactory      LoggerFactory
	onNewInvocationPair  func(prevInvocationID, parentID, childID string, downloadBytes, uploadBytes int64)
	onShutdown         func(invocationID string, downloadBytes, uploadBytes int64)
	idleTimer          *time.Timer
	sessionState       *sessionState
	config             ccacheconfig.Config
	metadata           configcommon.CacheConfigMetadata
	timerMutex         sync.Mutex
	capabilitiesOnce   sync.Once
	capabilitiesErr    error
	reportOnce         sync.Once
	activeInvocationID string
	activeInvocationMu sync.Mutex
}

func NewServer(
	config ccacheconfig.Config,
	metadata configcommon.CacheConfigMetadata,
	client Client,
	logger log.Logger,
	loggerFactory LoggerFactory,
	initialInvocationID string,
	onNewInvocationPair func(prevInvocationID, parentID, childID string, downloadBytes, uploadBytes int64),
	onShutdown func(invocationID string, downloadBytes, uploadBytes int64),
) (*IpcServer, error) {
	return &IpcServer{
		config:             config,
		metadata:           metadata,
		client:             client,
		logger:             logger,
		loggerFactory:      loggerFactory,
		onNewInvocationPair:  onNewInvocationPair,
		onShutdown:         onShutdown,
		sessionState:       newSessionState(),
		activeInvocationID: initialInvocationID,
	}, nil
}

func (s *IpcServer) Run(ctx context.Context) error {
	cancellableCtx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	listener, err := s.createListener(ctx)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	s.listener = listener
	defer s.listener.Close()

	s.logger.TInfof("Server listening on %s", s.config.IPCEndpoint)
	s.resetIdleTimer(cancelFn)
	go s.acceptLoop(cancellableCtx, cancelFn)
	<-cancellableCtx.Done() // wait for context cancellation
	s.logger.TInfof("Server shutting down")
	s.listener.Close()

	// If shutdown was triggered by idle timeout (not by a STOP request), fire the final report now.
	// resetAndGet must be called inside the mutex so that bytes and activeInvocationID are captured
	// atomically — a concurrent SetInvocationID goroutine must not be able to interleave between them.
	s.activeInvocationMu.Lock()
	dl, ul := s.sessionState.resetAndGet()
	activeID := s.activeInvocationID
	s.activeInvocationMu.Unlock()
	s.reportOnce.Do(func() {
		if s.onShutdown != nil {
			s.onShutdown(activeID, dl, ul)
		}
	})

	return nil
}

func (s *IpcServer) acceptLoop(ctx context.Context, cancelFn context.CancelFunc) {
	for {
		conn, err := s.listener.Accept()
		conID := uuid.Must(uuid.NewV4()).String()[:8]
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				s.logger.TErrorf("Accept error: %v", err)

				continue
			}
		}

		s.logger.TDebugf("[%s] Client connected", conID)
		s.resetIdleTimer(cancelFn)

		go s.handleConnection(ctx, cancelFn, conn, conID)
	}
}

func (s *IpcServer) getCapabilities(ctx context.Context) error {
	s.capabilitiesOnce.Do(func() {
		s.capabilitiesErr = s.client.GetCapabilitiesWithRetry(ctx)
	})

	return s.capabilitiesErr
}

func (s *IpcServer) handleConnection(ctx context.Context, cancelFn context.CancelFunc, conn net.Conn, conID string) {
	defer conn.Close()

	if err := protocol.WriteGreeting(conn); err != nil {
		s.logger.TErrorf("Failed to send greeting: %v", err)

		return
	}

	processor := newRequestProcessor(conn, s.config, s.metadata, s.client, s.logger, s.loggerFactory, s.getCapabilities)

	if err := processor.initCapabilities(ctx); err != nil {
		s.logger.TErrorf("[%s] Capabilities check failed: %v", conID, err)

		return
	}

	for {
		result := processor.processRequest(ctx)
		s.sessionState.updateWithResult(result)

		if result.CallStats.method == CALL_METHOD_SET_INVOCATION_ID && result.Outcome == PROCESS_REQUEST_OK {
			s.activeInvocationMu.Lock()
			dl, ul := s.sessionState.resetAndGet()
			prevID := s.activeInvocationID
			s.activeInvocationID = result.InvocationChildID
			s.activeInvocationMu.Unlock()
			if s.onNewInvocationPair != nil {
				s.onNewInvocationPair(prevID, result.InvocationParentID, result.InvocationChildID, dl, ul)
			}
		}

		if result.CallStats.method == CALL_METHOD_GET_SESSION_STATS && result.Outcome == PROCESS_REQUEST_OK {
			s.handleGetSessionStatsResult(conn, conID)
		}

		if result.CallStats.method == CALL_METHOD_STOP && result.Outcome == PROCESS_REQUEST_OK {
			s.activeInvocationMu.Lock()
			dl, ul := s.sessionState.resetAndGet()
			activeID := s.activeInvocationID
			s.activeInvocationMu.Unlock()
			s.reportOnce.Do(func() {
				if s.onShutdown != nil {
					s.onShutdown(activeID, dl, ul)
				}
			})
			if err := protocol.WriteOK(conn); err != nil {
				s.logger.TErrorf("[%s] Failed to write STOP response: %v", conID, err)
			}
			cancelFn()

			return
		}

		if result.Err != nil {
			if errors.Is(result.Err, io.EOF) {
				s.logger.TDebugf("[%s] Client disconnected", conID)
			} else {
				s.logger.TErrorf("[%s] Processing error: %v", conID, result.Err)
			}

			return
		}

		s.resetIdleTimer(cancelFn)
	}
}

func (s *IpcServer) handleGetSessionStatsResult(conn net.Conn, conID string) {
	s.activeInvocationMu.Lock()
	dl := s.sessionState.downloadBytes.Load()
	ul := s.sessionState.uploadBytes.Load()
	s.activeInvocationMu.Unlock()

	if err := protocol.WriteSessionStats(conn, dl, ul); err != nil {
		s.logger.TErrorf("[%s] Failed to write session stats response: %v", conID, err)
	}
}

// SessionBytes returns the accumulated bytes downloaded and uploaded since the last SetInvocationID reset.
func (s *IpcServer) SessionBytes() (int64, int64) {
	return s.sessionState.downloadBytes.Load(), s.sessionState.uploadBytes.Load()
}

func (s *IpcServer) resetIdleTimer(cancelFn context.CancelFunc) {
	if s.config.IdleTimeout == 0 {
		return
	}

	s.timerMutex.Lock()
	defer s.timerMutex.Unlock()

	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}

	s.idleTimer = time.AfterFunc(s.config.IdleTimeout, func() {
		s.logger.TInfof("Idle timeout reached, shutting down")
		cancelFn()
	})
}
