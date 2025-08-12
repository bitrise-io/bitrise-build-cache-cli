package tests

import (
	"errors"
	"testing"

	gotuilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/bitrise-io/xcelerate/cmd"
	internalMocks "github.com/bitrise-io/xcelerate/internal/mocks"
	"github.com/c2fo/testify/mock"
	"github.com/c2fo/testify/require"
	"github.com/stretchr/testify/assert"
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

		xcodeArgProvider := internalMocks.XcodeArgProviderMock{
			XcodeArgsFunc: func() []string { return xcodeArgs },
		}

		xcodeRunner := internalMocks.XcodeRunnerMock{
			RunXcodeFunc: func(args []string) error { return nil },
		}

		SUT := cmd.XcodebuildCmdFn

		// When
		_ = SUT(&logger, &xcodeRunner, &xcodeArgProvider)

		// Then
		assert.Len(t, xcodeArgProvider.XcodeArgsCalls(), 1)
		require.Len(t, xcodeRunner.RunXcodeCalls(), 1)
		assert.Equal(t, xcodeArgs, xcodeRunner.RunXcodeCalls()[0].Args)

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

		xcodeArgProvider := internalMocks.XcodeArgProviderMock{
			XcodeArgsFunc: func() []string { return xcodeArgs },
		}

		xcodeRunner := internalMocks.XcodeRunnerMock{
			RunXcodeFunc: func(args []string) error { return expected },
		}

		SUT := cmd.XcodebuildCmdFn

		// When
		actual := SUT(&logger, &xcodeRunner, &xcodeArgProvider)

		// Then
		assert.Error(t, expected, actual)
	})
}
