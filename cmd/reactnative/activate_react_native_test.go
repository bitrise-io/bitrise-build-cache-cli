//go:build unit

package reactnative_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/reactnative"
)

func Test_ActivateReactNativeCmdFn(t *testing.T) {
	ctx := context.Background()
	noOp := func(_ context.Context, _ log.Logger) error { return nil }

	t.Run("all sub-systems are activated when all flags are true", func(t *testing.T) {
		gradleCalled, xcodeCalled, cppCalled, helperCalled := false, false, false, false

		err := reactnative.ActivateReactNativeCmdFn(
			ctx,
			mockLogger,
			true, true, true,
			func(_ log.Logger) error { gradleCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { xcodeCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { cppCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { helperCalled = true; return nil },
		)

		require.NoError(t, err)
		assert.True(t, gradleCalled)
		assert.True(t, xcodeCalled)
		assert.True(t, cppCalled)
		assert.True(t, helperCalled)
	})

	t.Run("no sub-system is activated when all flags are false", func(t *testing.T) {
		gradleCalled, xcodeCalled, cppCalled, helperCalled := false, false, false, false

		err := reactnative.ActivateReactNativeCmdFn(
			ctx,
			mockLogger,
			false, false, false,
			func(_ log.Logger) error { gradleCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { xcodeCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { cppCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { helperCalled = true; return nil },
		)

		require.NoError(t, err)
		assert.False(t, gradleCalled)
		assert.False(t, xcodeCalled)
		assert.False(t, cppCalled)
		assert.False(t, helperCalled)
	})

	t.Run("only Gradle is activated when only gradle flag is true", func(t *testing.T) {
		gradleCalled, xcodeCalled, cppCalled, helperCalled := false, false, false, false

		err := reactnative.ActivateReactNativeCmdFn(
			ctx,
			mockLogger,
			true, false, false,
			func(_ log.Logger) error { gradleCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { xcodeCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { cppCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { helperCalled = true; return nil },
		)

		require.NoError(t, err)
		assert.True(t, gradleCalled)
		assert.False(t, xcodeCalled)
		assert.False(t, cppCalled)
		assert.False(t, helperCalled)
	})

	t.Run("only Xcode is activated when only xcode flag is true", func(t *testing.T) {
		gradleCalled, xcodeCalled, cppCalled, helperCalled := false, false, false, false

		err := reactnative.ActivateReactNativeCmdFn(
			ctx,
			mockLogger,
			false, true, false,
			func(_ log.Logger) error { gradleCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { xcodeCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { cppCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { helperCalled = true; return nil },
		)

		require.NoError(t, err)
		assert.False(t, gradleCalled)
		assert.True(t, xcodeCalled)
		assert.False(t, cppCalled)
		assert.False(t, helperCalled)
	})

	t.Run("only C++ is activated when only cpp flag is true", func(t *testing.T) {
		gradleCalled, xcodeCalled, cppCalled, helperCalled := false, false, false, false

		err := reactnative.ActivateReactNativeCmdFn(
			ctx,
			mockLogger,
			false, false, true,
			func(_ log.Logger) error { gradleCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { xcodeCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { cppCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { helperCalled = true; return nil },
		)

		require.NoError(t, err)
		assert.False(t, gradleCalled)
		assert.False(t, xcodeCalled)
		assert.True(t, cppCalled)
		assert.True(t, helperCalled)
	})

	t.Run("storage helper is started after cpp activation", func(t *testing.T) {
		var callOrder []string

		err := reactnative.ActivateReactNativeCmdFn(
			ctx,
			mockLogger,
			false, false, true,
			func(_ log.Logger) error { return nil },
			noOp,
			func(_ context.Context, _ log.Logger) error { callOrder = append(callOrder, "cpp"); return nil },
			func(_ context.Context, _ log.Logger) error { callOrder = append(callOrder, "helper"); return nil },
		)

		require.NoError(t, err)
		assert.Equal(t, []string{"cpp", "helper"}, callOrder)
	})

	t.Run("Gradle error is propagated and halts activation", func(t *testing.T) {
		gradleErr := errors.New("gradle failed")
		xcodeCalled, cppCalled, helperCalled := false, false, false

		err := reactnative.ActivateReactNativeCmdFn(
			ctx,
			mockLogger,
			true, true, true,
			func(_ log.Logger) error { return gradleErr },
			func(_ context.Context, _ log.Logger) error { xcodeCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { cppCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { helperCalled = true; return nil },
		)

		assert.ErrorContains(t, err, "gradle failed")
		assert.False(t, xcodeCalled)
		assert.False(t, cppCalled)
		assert.False(t, helperCalled)
	})

	t.Run("Xcode error is propagated and halts activation", func(t *testing.T) {
		xcodeErr := errors.New("xcode failed")
		cppCalled, helperCalled := false, false

		err := reactnative.ActivateReactNativeCmdFn(
			ctx,
			mockLogger,
			true, true, true,
			func(_ log.Logger) error { return nil },
			func(_ context.Context, _ log.Logger) error { return xcodeErr },
			func(_ context.Context, _ log.Logger) error { cppCalled = true; return nil },
			func(_ context.Context, _ log.Logger) error { helperCalled = true; return nil },
		)

		assert.ErrorContains(t, err, "xcode failed")
		assert.False(t, cppCalled)
		assert.False(t, helperCalled)
	})

	t.Run("C++ error is propagated and storage helper is not started", func(t *testing.T) {
		cppErr := errors.New("ccache failed")
		helperCalled := false

		err := reactnative.ActivateReactNativeCmdFn(
			ctx,
			mockLogger,
			true, true, true,
			func(_ log.Logger) error { return nil },
			noOp,
			func(_ context.Context, _ log.Logger) error { return cppErr },
			func(_ context.Context, _ log.Logger) error { helperCalled = true; return nil },
		)

		assert.ErrorContains(t, err, "ccache failed")
		assert.False(t, helperCalled)
	})

	t.Run("storage helper error is propagated", func(t *testing.T) {
		helperErr := errors.New("helper failed")

		err := reactnative.ActivateReactNativeCmdFn(
			ctx,
			mockLogger,
			false, false, true,
			func(_ log.Logger) error { return nil },
			noOp,
			func(_ context.Context, _ log.Logger) error { return nil },
			func(_ context.Context, _ log.Logger) error { return helperErr },
		)

		assert.ErrorContains(t, err, "helper failed")
	})

	t.Run("logger and context are forwarded to sub-system functions", func(t *testing.T) {
		var capturedCtx context.Context
		var capturedLogger log.Logger

		_ = reactnative.ActivateReactNativeCmdFn(
			ctx,
			mockLogger,
			false, true, false,
			func(_ log.Logger) error { return nil },
			func(c context.Context, l log.Logger) error {
				capturedCtx = c
				capturedLogger = l
				return nil
			},
			noOp,
			noOp,
		)

		assert.Equal(t, ctx, capturedCtx)
		assert.Equal(t, mockLogger, capturedLogger)
	})
}
