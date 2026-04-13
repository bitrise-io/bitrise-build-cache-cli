//go:build unit

// nolint: goconst
package xcode_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/xcode"
	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/xcode/mocks"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/analytics"
)

func Test_SaveInvocationAndRelation(t *testing.T) {
	ctx := context.Background()
	invocationID := "child-inv-id"

	t.Run("sends relation when PutInvocation succeeds and parent ID is set", func(t *testing.T) {
		t.Setenv("BITRISE_INVOCATION_ID", "parent-inv-id")

		saver := &mocks.InvocationSaverMock{
			PutInvocationFunc: func(_ xa.Invocation) error { return nil },
		}

		var capturedParent, capturedChild string
		sendRelation := xcode.SendRelationFn(func(_ context.Context, _ log.Logger, parentID, childID string) error {
			capturedParent = parentID
			capturedChild = childID

			return nil
		})

		xcode.SaveInvocationAndRelation(ctx, mockLogger, saver, xa.Invocation{}, invocationID, sendRelation)

		require.Len(t, saver.PutInvocationCalls(), 1)
		assert.Equal(t, "parent-inv-id", capturedParent)
		assert.Equal(t, invocationID, capturedChild)
	})

	t.Run("does not send relation when parent ID is not set", func(t *testing.T) {
		t.Setenv("BITRISE_INVOCATION_ID", "")

		saver := &mocks.InvocationSaverMock{
			PutInvocationFunc: func(_ xa.Invocation) error { return nil },
		}

		relationCalled := false
		sendRelation := xcode.SendRelationFn(func(_ context.Context, _ log.Logger, _, _ string) error {
			relationCalled = true

			return nil
		})

		xcode.SaveInvocationAndRelation(ctx, mockLogger, saver, xa.Invocation{}, invocationID, sendRelation)

		require.Len(t, saver.PutInvocationCalls(), 1)
		assert.False(t, relationCalled, "sendRelationFn must not be called when BITRISE_INVOCATION_ID is empty")
	})

	t.Run("does not send relation when PutInvocation fails", func(t *testing.T) {
		t.Setenv("BITRISE_INVOCATION_ID", "parent-inv-id")

		saver := &mocks.InvocationSaverMock{
			PutInvocationFunc: func(_ xa.Invocation) error { return errors.New("analytics down") },
		}

		relationCalled := false
		sendRelation := xcode.SendRelationFn(func(_ context.Context, _ log.Logger, _, _ string) error {
			relationCalled = true

			return nil
		})

		xcode.SaveInvocationAndRelation(ctx, mockLogger, saver, xa.Invocation{}, invocationID, sendRelation)

		require.Len(t, saver.PutInvocationCalls(), 1)
		assert.False(t, relationCalled, "sendRelationFn must not be called when PutInvocation fails")
	})

	t.Run("does not send relation when sendRelationFn is nil", func(t *testing.T) {
		t.Setenv("BITRISE_INVOCATION_ID", "parent-inv-id")

		saver := &mocks.InvocationSaverMock{
			PutInvocationFunc: func(_ xa.Invocation) error { return nil },
		}

		xcode.SaveInvocationAndRelation(ctx, mockLogger, saver, xa.Invocation{}, invocationID, nil)

		require.Len(t, saver.PutInvocationCalls(), 1)
		// no panic — nil sendRelationFn is handled gracefully
	})

	t.Run("relation error is logged but does not propagate", func(t *testing.T) {
		t.Setenv("BITRISE_INVOCATION_ID", "parent-inv-id")

		saver := &mocks.InvocationSaverMock{
			PutInvocationFunc: func(_ xa.Invocation) error { return nil },
		}

		sendRelation := xcode.SendRelationFn(func(_ context.Context, _ log.Logger, _, _ string) error {
			return errors.New("relation failed")
		})

		// Should not panic — error is logged internally
		xcode.SaveInvocationAndRelation(ctx, mockLogger, saver, xa.Invocation{}, invocationID, sendRelation)

		require.Len(t, saver.PutInvocationCalls(), 1)
	})
}
