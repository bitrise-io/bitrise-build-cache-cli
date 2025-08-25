package cmd_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd"
	cmdMocks "github.com/bitrise-io/bitrise-build-cache-cli/cmd/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/xcodeargs/mocks"
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
			ArgsFunc: func(_ map[string]string) []string { return xcodeArgs },
		}

		xcodeRunner := &cmdMocks.XcodeRunnerMock{
			RunFunc: func(_ context.Context, _ []string) error { return nil },
		}

		SUT := cmd.XcodebuildCmdFn

		// When
		_ = SUT(context.Background(), mockLogger, xcodeRunner, xcelerate.Config{}, &xcodeArgProvider)

		// Then
		assert.Len(t, xcodeArgProvider.ArgsCalls(), 1)
		require.Len(t, xcodeRunner.RunCalls(), 1)
		assert.Equal(t, xcodeArgs, xcodeRunner.RunCalls()[0].Args)

		mockLogger.AssertCalled(t, "TDebugf", cmd.MsgArgsPassedToXcodebuild, xcodeArgs)
	})

	t.Run("xcodebuildCmdFn returns any error happened in XcodeRunner", func(t *testing.T) {
		// Given
		expected := errors.New("testError")

		xcodeArgs := []string{}

		xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
			ArgsFunc: func(_ map[string]string) []string { return xcodeArgs },
		}

		xcodeRunner := &cmdMocks.XcodeRunnerMock{
			RunFunc: func(_ context.Context, _ []string) error { return expected },
		}

		SUT := cmd.XcodebuildCmdFn

		// When
		actual := SUT(context.Background(), mockLogger, xcodeRunner, xcelerate.Config{}, &xcodeArgProvider)

		// Then
		require.EqualError(t, actual, expected.Error())
	})
}
