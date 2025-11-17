// nolint: goconst
package xcode_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/xcode"
	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/xcode/mocks"
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

		sessionClientMock := &mocks.SessionClientMock{
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

		xcodeRunner := &mocks.XcodeRunnerMock{
			RunFunc: func(_ context.Context, _ []string) xcodeargs.RunStats { return xcodeargs.RunStats{} },
		}

		SUT := xcode.XcodebuildCmdFn

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

		mockLogger.AssertCalled(t, "TDebugf", xcode.MsgArgsPassedToXcodebuild, xcodeArgs)
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

		xcodeRunner := &mocks.XcodeRunnerMock{
			RunFunc: func(_ context.Context, _ []string) xcodeargs.RunStats {
				return xcodeargs.RunStats{Error: expected, ExitCode: 55}
			},
		}

		sessionClientMock := &mocks.SessionClientMock{
			GetSessionStatsFunc: func(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*session.GetSessionStatsResponse, error) {
				return &session.GetSessionStatsResponse{}, nil
			},
			SetSessionFunc: func(ctx context.Context, in *session.SetSessionRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
				return &emptypb.Empty{}, nil
			},
		}

		SUT := xcode.XcodebuildCmdFn

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
		require.EqualError(t, actual.Error, expected.Error())
		require.Equal(t, 55, actual.ExitCode)
	})

	t.Run("xcodebuildCmdFn adds cache flags only if build cache is enabled and skip flag is not enabled", func(t *testing.T) {
		testCases := []struct {
			name                string
			buildCacheEnabled   bool
			buildCacheSkipFlags bool
			expectCacheFlags    bool
		}{
			{"enabled & not skipped", true, false, true},
			{"enabled & skipped", true, true, false},
			{"disabled & not skipped", false, false, false},
			{"disabled & skipped", false, true, false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				var receivedAdditional map[string]string
				xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
					ArgsFunc: func(additional map[string]string) []string {
						receivedAdditional = additional

						return []string{"xcodebuild"}
					},
					CommandFunc:      func() string { return "xcodebuild" },
					ShortCommandFunc: func() string { return "xcodebuild" },
				}

				xcodeRunner := &mocks.XcodeRunnerMock{
					RunFunc: func(_ context.Context, args []string) xcodeargs.RunStats { return xcodeargs.RunStats{} },
				}

				sessionClientMock := &mocks.SessionClientMock{}

				SUT := xcode.XcodebuildCmdFn
				config := xcelerate.Config{
					BuildCacheEnabled:   tc.buildCacheEnabled,
					BuildCacheSkipFlags: tc.buildCacheSkipFlags,
					ProxySocketPath:     "/tmp/proxy.sock",
				}

				_ = SUT(
					context.Background(),
					uuid.NewString(),
					mockLogger,
					xcodeRunner,
					sessionClientMock,
					config,
					common.CacheConfigMetadata{},
					&xcodeArgProvider,
				)

				for k, v := range xcodeargs.CacheArgs {
					if tc.expectCacheFlags {
						assert.Equal(t, v, receivedAdditional[k], "Expected cache flag %s to be present", k)
					} else {
						assert.NotContains(t, receivedAdditional, k, "Did not expect cache flag %s", k)
					}
				}
				if tc.buildCacheEnabled {
					assert.Equal(t, "/tmp/proxy.sock", receivedAdditional["COMPILATION_CACHE_REMOTE_SERVICE_PATH"], "Proxy path should be set when build cache is enabled")
				} else {
					assert.NotContains(t, receivedAdditional, "COMPILATION_CACHE_REMOTE_SERVICE_PATH", "Proxy path should not be set when build cache is disabled")
				}
			})
		}
	})

	t.Run("xcodebuildCmdFn sets diagnostic remarks only if debug flag is set", func(t *testing.T) {
		cases := []struct {
			name         string
			debugLogging bool
			expected     string
		}{
			{"debug enabled", true, "YES"},
			{"debug disabled", false, "NO"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var receivedAdditional map[string]string
				xcodeArgProvider := xcodeargsMocks.XcodeArgsMock{
					ArgsFunc: func(additional map[string]string) []string {
						receivedAdditional = additional

						return []string{"xcodebuild"}
					},
					CommandFunc:      func() string { return "xcodebuild" },
					ShortCommandFunc: func() string { return "xcodebuild" },
				}
				xcodeRunner := &mocks.XcodeRunnerMock{
					RunFunc: func(_ context.Context, args []string) xcodeargs.RunStats { return xcodeargs.RunStats{} },
				}
				sessionClientMock := &mocks.SessionClientMock{}
				SUT := xcode.XcodebuildCmdFn
				config := xcelerate.Config{
					BuildCacheEnabled:   true,
					BuildCacheSkipFlags: false,
					DebugLogging:        tc.debugLogging,
					ProxySocketPath:     "/tmp/proxy.sock",
				}
				_ = SUT(
					context.Background(),
					uuid.NewString(),
					mockLogger,
					xcodeRunner,
					sessionClientMock,
					config,
					common.CacheConfigMetadata{},
					&xcodeArgProvider,
				)
				assert.Equal(t, tc.expected, receivedAdditional["COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS"], "Diagnostic remarks should be %s if debug is %v", tc.expected, tc.debugLogging)
			})
		}
	})
}
