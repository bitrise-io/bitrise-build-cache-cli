package cmd_test

import (
	"errors"
	"testing"

	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/xcelerate/cmd"
	cmdMocks "github.com/bitrise-io/bitrise-build-cache-cli/xcelerate/cmd/mocks"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/xcelerate/xcodeargs/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_xcodebuildCmdFn(t *testing.T) {
	t.Run("xcodebuildCmdFn extracts the xcode args and passes them to xcode runner", func(t *testing.T) {
		// Given
		xcodeArgs := []string{
			"arg1",
			"--flag1",
			"arg2",
			"--flag2",
			"-v",
		}

		xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
			ArgsFunc: func() []string { return xcodeArgs },
		}

		xcodeRunner := &cmdMocks.XcodeRunnerMock{
			RunFunc: func(_ context.Context, _ []string) error { return nil },
		}

		SUT := cmd.XcodebuildCmdFn

		// When
		_ = SUT(context.Background(), mockLogger, xcodeRunner, &xcodeArgProvider)

		// Then
		assert.Len(t, xcodeArgProvider.ArgsCalls(), 1)
		require.Len(t, xcodeRunner.RunCalls(), 1)
		assert.Equal(t, xcodeArgs, xcodeRunner.RunCalls()[0].Args)

		mockLogger.AssertNumberOfCalls(t, "TDebugf", 1)
		mockLogger.AssertCalled(t, "TDebugf", cmd.MsgArgsPassedToXcodebuild, xcodeArgs)
	})

	t.Run("xcodebuildCmdFn returns any error happened in XcodeRunner", func(t *testing.T) {
		// Given
		expected := errors.New("testError")

		xcodeArgs := []string{}

		xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
			ArgsFunc: func() []string { return xcodeArgs },
		}

		xcodeRunner := &cmdMocks.XcodeRunnerMock{
			RunFunc: func(_ context.Context, _ []string) error { return expected },
		}

		SUT := cmd.XcodebuildCmdFn

		// When
		actual := SUT(context.Background(), mockLogger, xcodeRunner, &xcodeArgProvider)

		// Then
		require.EqualError(t, actual, expected.Error())
	})
}
