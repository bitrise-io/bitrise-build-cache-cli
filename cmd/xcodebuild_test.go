// nolint: goconst
package cmd_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd"
	cmdMocks "github.com/bitrise-io/bitrise-build-cache-cli/cmd/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/xcodeargs"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/xcodeargs/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/proto/llvm/session"
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

		sessionClientMock := &cmdMocks.SessionClientMock{
			GetSessionStatsFunc: func(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*session.GetSessionStatsResponse, error) {
				return &session.GetSessionStatsResponse{}, nil
			},
			SetSessionFunc: func(ctx context.Context, in *session.SetSessionRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
				return &emptypb.Empty{}, nil
			},
		}

		xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
			ArgsFunc: func(_ map[string]string) []string { return xcodeArgs },
			CommandFunc: func() string {
				return "xcodebuild"
			},
			ShortCommandFunc: func() string {
				return "xcodebuild"
			},
		}

		xcodeRunner := &cmdMocks.XcodeRunnerMock{
			RunFunc: func(_ context.Context, _ []string) xcodeargs.RunStats { return xcodeargs.RunStats{} },
		}

		SUT := cmd.XcodebuildCmdFn

		// When
		_ = SUT(
			context.Background(),
			uuid.NewString(),
			mockLogger,
			xcodeRunner,
			sessionClientMock,
			xcelerate.Config{},
			common.CacheConfigMetadata{},
			&xcodeArgProvider,
		)

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
			CommandFunc: func() string {
				return "xcodebuild"
			},
			ShortCommandFunc: func() string {
				return "xcodebuild"
			},
		}

		xcodeRunner := &cmdMocks.XcodeRunnerMock{
			RunFunc: func(_ context.Context, _ []string) xcodeargs.RunStats { return xcodeargs.RunStats{Error: expected} },
		}

		sessionClientMock := &cmdMocks.SessionClientMock{
			GetSessionStatsFunc: func(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*session.GetSessionStatsResponse, error) {
				return &session.GetSessionStatsResponse{}, nil
			},
			SetSessionFunc: func(ctx context.Context, in *session.SetSessionRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
				return &emptypb.Empty{}, nil
			},
		}

		SUT := cmd.XcodebuildCmdFn

		// When
		actual := SUT(
			context.Background(),
			uuid.NewString(),
			mockLogger,
			xcodeRunner,
			sessionClientMock,
			xcelerate.Config{},
			common.CacheConfigMetadata{},
			&xcodeArgProvider,
		)

		// Then
		require.EqualError(t, actual, expected.Error())
	})
}
