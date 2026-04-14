//go:build unit

package xcode

import (
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/analytics"
)

var relationTestLogger = newRelationTestLogger() //nolint:gochecknoglobals

func newRelationTestLogger() *utilsMocks.Logger {
	l := &utilsMocks.Logger{}
	l.On("Debugf", mock.Anything, mock.Anything).Return()
	l.On("Infof", mock.Anything, mock.Anything).Return()
	l.On("Errorf", mock.Anything, mock.Anything).Return()
	l.On("TInfof", mock.Anything, mock.Anything, mock.Anything).Return()
	l.On("TInfof", mock.Anything, mock.Anything).Return()
	l.On("TInfof", mock.Anything).Return()

	return l
}

func testInvocation() analytics.Invocation {
	return analytics.Invocation{InvocationID: "child-inv-id"}
}

type stubInvocationSaver struct {
	err error
}

func (s *stubInvocationSaver) PutInvocation(_ analytics.Invocation) error {
	return s.err
}

func Test_sendRelation(t *testing.T) {
	t.Run("sends relation with correct IDs and build tool", func(t *testing.T) {
		var captured multiplatform.InvocationRelation
		mock := &relationSenderMock{
			PutInvocationRelationFunc: func(rel multiplatform.InvocationRelation) error {
				captured = rel

				return nil
			},
		}

		runner := &XcodebuildRunner{
			Config:       xcelerate.Config{},
			InvocationID: "child-inv-id",
			Logger:       relationTestLogger,
			relationAPI:  mock,
		}

		runner.sendRelation("parent-inv-id")

		require.Len(t, mock.PutInvocationRelationCalls(), 1)
		assert.Equal(t, "parent-inv-id", captured.ParentInvocationID)
		assert.Equal(t, "child-inv-id", captured.ChildInvocationID)
		assert.Equal(t, "xcode", captured.BuildTool)
		assert.False(t, captured.InvocationDate.IsZero())
	})

	t.Run("logs error when PutInvocationRelation fails", func(t *testing.T) {
		mock := &relationSenderMock{
			PutInvocationRelationFunc: func(_ multiplatform.InvocationRelation) error {
				return assert.AnError
			},
		}

		runner := &XcodebuildRunner{
			Config:       xcelerate.Config{},
			InvocationID: "child-inv-id",
			Logger:       relationTestLogger,
			relationAPI:  mock,
		}

		runner.sendRelation("parent-inv-id")

		require.Len(t, mock.PutInvocationRelationCalls(), 1)
	})
}

func Test_saveInvocationAndRelation(t *testing.T) {
	t.Run("sends relation when invocation succeeds and parent ID is set", func(t *testing.T) {
		t.Setenv("BITRISE_INVOCATION_ID", "parent-inv-id")

		relationCalled := false
		relMock := &relationSenderMock{
			PutInvocationRelationFunc: func(_ multiplatform.InvocationRelation) error {
				relationCalled = true

				return nil
			},
		}

		runner := &XcodebuildRunner{
			Config:        xcelerate.Config{},
			InvocationID:  "child-inv-id",
			Logger:        relationTestLogger,
			invocationAPI: &stubInvocationSaver{},
			relationAPI:   relMock,
		}

		runner.saveInvocationAndRelation(testInvocation())

		assert.True(t, relationCalled)
	})

	t.Run("does not send relation when parent ID is not set", func(t *testing.T) {
		t.Setenv("BITRISE_INVOCATION_ID", "")

		relationCalled := false
		relMock := &relationSenderMock{
			PutInvocationRelationFunc: func(_ multiplatform.InvocationRelation) error {
				relationCalled = true

				return nil
			},
		}

		runner := &XcodebuildRunner{
			Config:        xcelerate.Config{},
			InvocationID:  "child-inv-id",
			Logger:        relationTestLogger,
			invocationAPI: &stubInvocationSaver{},
			relationAPI:   relMock,
		}

		runner.saveInvocationAndRelation(testInvocation())

		assert.False(t, relationCalled)
	})

	t.Run("does not send relation when PutInvocation fails", func(t *testing.T) {
		t.Setenv("BITRISE_INVOCATION_ID", "parent-inv-id")

		relationCalled := false
		relMock := &relationSenderMock{
			PutInvocationRelationFunc: func(_ multiplatform.InvocationRelation) error {
				relationCalled = true

				return nil
			},
		}

		runner := &XcodebuildRunner{
			Config:        xcelerate.Config{},
			InvocationID:  "child-inv-id",
			Logger:        relationTestLogger,
			invocationAPI: &stubInvocationSaver{err: assert.AnError},
			relationAPI:   relMock,
		}

		runner.saveInvocationAndRelation(testInvocation())

		assert.False(t, relationCalled)
	})
}
