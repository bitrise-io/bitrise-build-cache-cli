package cmd_test

import (
	"errors"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/xcelerate/cmd"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/xcelerate/xcodeargs/mocks"
	gotuilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

		logger := gotuilsMocks.Logger{}
		logger.On("TDebugf", mock.Anything).Return()
		logger.On("TDebugf", mock.Anything, mock.Anything).Return()

		xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
			ArgsFunc: func() []string { return xcodeArgs },
		}

		xcodeRunner := xcodeargsMocks.RunnerMock{
			RunFunc: func(_ []string) error { return nil },
		}

		SUT := cmd.XcodebuildCmdFn

		// When
		_ = SUT(&logger, &xcodeRunner, &xcodeArgProvider)

		// Then
		assert.Len(t, xcodeArgProvider.ArgsCalls(), 1)
		require.Len(t, xcodeRunner.RunCalls(), 1)
		assert.Equal(t, xcodeArgs, xcodeRunner.RunCalls()[0].Args)

		logger.AssertNumberOfCalls(t, "TDebugf", 1)
		logger.AssertCalled(t, "TDebugf", cmd.MsgArgsPassedToXcodebuild, xcodeArgs)
	})

	t.Run("xcodebuildCmdFn returns any error happened in XcodeRunner", func(t *testing.T) {
		// Given
		expected := errors.New("testError")

		xcodeArgs := []string{}

		logger := gotuilsMocks.Logger{}
		logger.On("TDebugf", mock.Anything).Return()
		logger.On("TDebugf", mock.Anything, mock.Anything).Return()

		xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
			ArgsFunc: func() []string { return xcodeArgs },
		}

		xcodeRunner := xcodeargsMocks.RunnerMock{
			RunFunc: func(_ []string) error { return expected },
		}

		SUT := cmd.XcodebuildCmdFn

		// When
		actual := SUT(&logger, &xcodeRunner, &xcodeArgProvider)

		// Then
		require.EqualError(t, actual, expected.Error())
	})
}
