// nolint: goconst
package xcode_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/xcode"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/xcode/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs/mocks"
	sessionproto "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
)

func Test_xcodebuildRunner_WorkspaceIDFlowsThroughSetSession(t *testing.T) {
	xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
		HasBuildActionFunc: func() bool { return true },
		ArgsFunc:           func(_ map[string]string) []string { return nil },
		CommandFunc:        func() string { return "xcodebuild" },
		ShortCommandFunc:   func() string { return "xcodebuild" },
	}

	xcodeRunner := &mocks.XcodeRunnerMock{
		RunFunc: func(_ context.Context, _ []string) xcodeargs.RunStats { return xcodeargs.RunStats{} },
	}

	sessionClientMock := &mocks.SessionClientMock{
		GetSessionStatsFunc: func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*sessionproto.GetSessionStatsResponse, error) {
			return &sessionproto.GetSessionStatsResponse{}, nil
		},
		SetSessionFunc: func(_ context.Context, _ *sessionproto.SetSessionRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			return &emptypb.Empty{}, nil
		},
	}

	SUT := &xcode.XcodebuildRunner{
		Config:             xcelerate.Config{SelfEnrichDisabled: true},
		Metadata:           common.CacheConfigMetadata{},
		InvocationID:       uuid.NewString(),
		WorkspaceID:        "acme-workspace",
		Logger:             mockLogger,
		CacheLogger:        mockLogger,
		XcodeRunner:        xcodeRunner,
		ProxySessionClient: sessionClientMock,
		XcodeArgs:          &xcodeArgProvider,
	}

	_ = SUT.Run(context.Background())

	calls := sessionClientMock.SetSessionCalls()
	require.Len(t, calls, 1)
	md, ok := metadata.FromOutgoingContext(calls[0].Ctx)
	require.True(t, ok, "SetSession call must carry outgoing gRPC metadata")
	assert.Equal(t, []string{"acme-workspace"}, md.Get("x-bitrise-workspace-id"))
}

func Test_xcodebuildRunner_EmptyWorkspaceIDLeavesCtxClean(t *testing.T) {
	xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
		HasBuildActionFunc: func() bool { return true },
		ArgsFunc:           func(_ map[string]string) []string { return nil },
		CommandFunc:        func() string { return "xcodebuild" },
		ShortCommandFunc:   func() string { return "xcodebuild" },
	}

	xcodeRunner := &mocks.XcodeRunnerMock{
		RunFunc: func(_ context.Context, _ []string) xcodeargs.RunStats { return xcodeargs.RunStats{} },
	}

	sessionClientMock := &mocks.SessionClientMock{
		GetSessionStatsFunc: func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*sessionproto.GetSessionStatsResponse, error) {
			return &sessionproto.GetSessionStatsResponse{}, nil
		},
		SetSessionFunc: func(_ context.Context, _ *sessionproto.SetSessionRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			return &emptypb.Empty{}, nil
		},
	}

	SUT := &xcode.XcodebuildRunner{
		Config:             xcelerate.Config{SelfEnrichDisabled: true},
		Metadata:           common.CacheConfigMetadata{},
		InvocationID:       uuid.NewString(),
		Logger:             mockLogger,
		CacheLogger:        mockLogger,
		XcodeRunner:        xcodeRunner,
		ProxySessionClient: sessionClientMock,
		XcodeArgs:          &xcodeArgProvider,
	}

	_ = SUT.Run(context.Background())

	calls := sessionClientMock.SetSessionCalls()
	require.Len(t, calls, 1)
	if md, ok := metadata.FromOutgoingContext(calls[0].Ctx); ok {
		assert.Empty(t, md.Get("x-bitrise-workspace-id"))
	}
}
