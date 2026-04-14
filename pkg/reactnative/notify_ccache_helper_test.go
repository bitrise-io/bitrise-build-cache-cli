//go:build unit

package reactnative

import (
	"context"
	"errors"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
)

var helperTestLogger = log.NewLogger() //nolint:gochecknoglobals

const (
	testRNInvocationID     = "rn-invocation-id"
	testCcacheInvocationID = "ccache-invocation-id"
)

type stubSocket struct {
	listening          bool
	startErr           error
	awaitResult        bool
	healthCheckErr     error
	setInvocationIDErr error

	startCalled          bool
	awaitCalled          bool
	healthCheckCalled    bool
	setInvocationCalled  bool
	capturedParentID     string
	capturedChildID      string
}

func (s *stubSocket) IsListening() bool                        { return s.listening }
func (s *stubSocket) Start() error                             { s.startCalled = true; return s.startErr }
func (s *stubSocket) AwaitReady() bool                         { s.awaitCalled = true; return s.awaitResult }
func (s *stubSocket) HealthCheck(_ context.Context) error      { s.healthCheckCalled = true; return s.healthCheckErr }
func (s *stubSocket) SetInvocationID(_ context.Context, parentID, childID string) error {
	s.setInvocationCalled = true
	s.capturedParentID = parentID
	s.capturedChildID = childID

	return s.setInvocationIDErr
}

func newTestRunnerWithSocket(socket ccacheSocket) *Runner {
	return &Runner{
		ccacheInvocationID: testCcacheInvocationID,
		logger:             helperTestLogger,
		socket:             socket,
	}
}

func TestEnsureHelper(t *testing.T) {
	t.Run("nil socket means Run skips ensureHelper entirely", func(t *testing.T) {
		r := newTestRunnerWithSocket(nil)
		// socket is nil, so ensureHelper is never called — just verify no panic
		assert.Nil(t, r.socket)
	})

	t.Run("does not start helper when socket is already listening", func(t *testing.T) {
		s := &stubSocket{listening: true, awaitResult: true}
		r := newTestRunnerWithSocket(s)

		r.ensureHelper(testRNInvocationID)

		assert.False(t, s.startCalled)
		assert.True(t, s.healthCheckCalled)
		assert.True(t, s.setInvocationCalled)
	})

	t.Run("starts helper and waits when socket is not listening", func(t *testing.T) {
		s := &stubSocket{listening: false, awaitResult: true}
		r := newTestRunnerWithSocket(s)

		r.ensureHelper(testRNInvocationID)

		assert.True(t, s.startCalled)
		assert.True(t, s.awaitCalled)
		assert.True(t, s.healthCheckCalled)
	})

	t.Run("does not await when start helper fails", func(t *testing.T) {
		s := &stubSocket{listening: false, startErr: errors.New("start failed")}
		r := newTestRunnerWithSocket(s)

		r.ensureHelper(testRNInvocationID)

		assert.True(t, s.startCalled)
		assert.False(t, s.awaitCalled)
		assert.False(t, s.healthCheckCalled)
		assert.False(t, s.setInvocationCalled)
	})

	t.Run("continues without error when AwaitReady returns false", func(t *testing.T) {
		s := &stubSocket{listening: false, awaitResult: false}
		r := newTestRunnerWithSocket(s)

		r.ensureHelper(testRNInvocationID)

		assert.True(t, s.awaitCalled)
		assert.True(t, s.healthCheckCalled) // still proceeds
	})

	t.Run("continues when HealthCheck fails", func(t *testing.T) {
		s := &stubSocket{listening: true, healthCheckErr: errors.New("unhealthy")}
		r := newTestRunnerWithSocket(s)

		r.ensureHelper(testRNInvocationID)

		assert.True(t, s.healthCheckCalled)
		assert.True(t, s.setInvocationCalled, "SetInvocationID should still be called after a failed health check")
	})

	t.Run("calls SetInvocationID with correct IDs", func(t *testing.T) {
		s := &stubSocket{listening: true}
		r := newTestRunnerWithSocket(s)

		r.ensureHelper(testRNInvocationID)

		assert.Equal(t, testRNInvocationID, s.capturedParentID)
		assert.Equal(t, testCcacheInvocationID, s.capturedChildID)
	})
}
