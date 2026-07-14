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

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/xcode"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/xcode/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
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

		SUT := &xcode.XcodebuildRunner{
			Config:             xcelerate.Config{},
			Metadata:           common.CacheConfigMetadata{},
			InvocationID:       uuid.NewString(),
			Logger:             mockLogger,
			CacheLogger:        mockLogger,
			XcodeRunner:        xcodeRunner,
			ProxySessionClient: sessionClientMock,
			XcodeArgs:          &xcodeArgProvider,
		}

		// When
		_ = SUT.Run(context.Background())

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

		SUT := &xcode.XcodebuildRunner{
			Config:             xcelerate.Config{},
			Metadata:           common.CacheConfigMetadata{},
			InvocationID:       uuid.NewString(),
			Logger:             mockLogger,
			CacheLogger:        mockLogger,
			XcodeRunner:        xcodeRunner,
			ProxySessionClient: sessionClientMock,
			XcodeArgs:          &xcodeArgProvider,
		}

		// When
		actual := SUT.Run(context.Background())

		// Then
		require.EqualError(t, actual.Error, expected.Error())
		require.Equal(t, 55, actual.ExitCode)
	})

	t.Run("xcodebuildCmdFn adds cache flags only if build cache is enabled and skip flag is not enabled", func(t *testing.T) {
		testCases := []struct {
			name                 string
			buildCacheEnabled    bool
			buildCacheSkipFlags  bool
			disablePrefixMapping bool
			expectCacheFlags     bool
			expectPrefixMaps     bool
		}{
			{"enabled & not skipped", true, false, false, true, true},
			{"enabled & skipped", true, true, false, false, false},
			{"enabled, prefix mapping disabled", true, false, true, true, false},
			{"disabled & not skipped", false, false, false, false, false},
			{"disabled & skipped", false, true, false, false, false},
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

				SUT := &xcode.XcodebuildRunner{
					Config: xcelerate.Config{
						BuildCacheEnabled:    tc.buildCacheEnabled,
						BuildCacheSkipFlags:  tc.buildCacheSkipFlags,
						DisablePrefixMapping: tc.disablePrefixMapping,
						ProxySocketPath:      "/tmp/proxy.sock",
					},
					Metadata:           common.CacheConfigMetadata{},
					InvocationID:       uuid.NewString(),
					Logger:             mockLogger,
					CacheLogger:        mockLogger,
					XcodeRunner:        xcodeRunner,
					ProxySessionClient: sessionClientMock,
					XcodeArgs:          &xcodeArgProvider,
				}

				_ = SUT.Run(context.Background())

				for k, v := range xcodeargs.CacheArgs {
					if tc.expectCacheFlags {
						assert.Equal(t, v, receivedAdditional[k], "Expected cache flag %s to be present", k)
					} else {
						assert.NotContains(t, receivedAdditional, k, "Did not expect cache flag %s", k)
					}
				}
				if tc.expectPrefixMaps {
					assert.Equal(t, "YES", receivedAdditional[xcodeargs.ClangEnablePrefixMappingKey], "Expected CLANG_ENABLE_PREFIX_MAPPING=YES in build settings")
				} else {
					assert.NotContains(t, receivedAdditional, xcodeargs.ClangEnablePrefixMappingKey, "Did not expect CLANG_ENABLE_PREFIX_MAPPING in build settings")
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
				SUT := &xcode.XcodebuildRunner{
					Config: xcelerate.Config{
						BuildCacheEnabled:   true,
						BuildCacheSkipFlags: false,
						DebugLogging:        tc.debugLogging,
						ProxySocketPath:     "/tmp/proxy.sock",
					},
					Metadata:           common.CacheConfigMetadata{},
					InvocationID:       uuid.NewString(),
					Logger:             mockLogger,
					CacheLogger:        mockLogger,
					XcodeRunner:        xcodeRunner,
					ProxySessionClient: sessionClientMock,
					XcodeArgs:          &xcodeArgProvider,
				}
				_ = SUT.Run(context.Background())
				assert.Equal(t, tc.expected, receivedAdditional["COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS"], "Diagnostic remarks should be %s if debug is %v", tc.expected, tc.debugLogging)
			})
		}
	})
}

func Test_assembleArgs_prefixMapInjection(t *testing.T) {
	newRunnerWithArgs := func(cfg xcelerate.Config, argsMock *xcodeargsMocks.XcodeArgsMock, capturedArgs *[]string, opts ...func(*xcode.XcodebuildRunner)) *xcode.XcodebuildRunner {
		r := &xcode.XcodebuildRunner{
			Config:       cfg,
			Metadata:     common.CacheConfigMetadata{},
			InvocationID: uuid.NewString(),
			Logger:       mockLogger,
			CacheLogger:  mockLogger,
			XcodeRunner: &mocks.XcodeRunnerMock{
				RunFunc: func(_ context.Context, args []string) xcodeargs.RunStats {
					*capturedArgs = append([]string(nil), args...)

					return xcodeargs.RunStats{}
				},
			},
			ProxySessionClient: &mocks.SessionClientMock{},
			XcodeArgs:          argsMock,
			Paths:              paths.FromHome("/h"),
		}
		for _, o := range opts {
			o(r)
		}

		return r
	}

	t.Run("injects wrapper-owned DerivedData when user did not pass one", func(t *testing.T) {
		var receivedAdditional map[string]string
		argsMock := &xcodeargsMocks.XcodeArgsMock{
			ArgsFunc: func(additional map[string]string) []string {
				receivedAdditional = additional

				return []string{"xcodebuild"}
			},
			ProjectDirFunc:      func() string { return "/work/app" },
			DerivedDataPathFunc: func() string { return "" },
			ProjectTempDirFunc:  func() string { return "" },
			UserOtherCFlagsFunc: func() string { return "" },
			CommandFunc:         func() string { return "xcodebuild" },
			ShortCommandFunc:    func() string { return "xcodebuild" },
		}
		var captured []string
		r := newRunnerWithArgs(xcelerate.Config{
			BuildCacheEnabled: true,
			ProxySocketPath:   "/tmp/proxy.sock",
		}, argsMock, &captured)

		_ = r.Run(context.Background())

		assert.Equal(t, "YES", receivedAdditional[xcodeargs.ClangEnablePrefixMappingKey])
		assert.Contains(t, receivedAdditional, xcodeargs.ProjectTempDirKey, "PROJECT_TEMP_DIR build setting should be injected")
		assert.Contains(t, receivedAdditional[xcodeargs.ProjectTempDirKey], "/h/.bitrise/cache/xcode-ptd/")

		// -derivedDataPath is appended to argv, not to build settings.
		require.Contains(t, captured, xcodeargs.DerivedDataPathFlag)
		idx := indexOf(captured, xcodeargs.DerivedDataPathFlag)
		require.Less(t, idx+1, len(captured))
		assert.Contains(t, captured[idx+1], "/h/.bitrise/cache/xcode-dd/")

		// OTHER_CFLAGS appended with prefix-map suffix rooted at $(inherited).
		other := findBuildSetting(captured, xcodeargs.OtherCFlagsKey)
		require.NotEmpty(t, other, "OTHER_CFLAGS should be present in argv")
		assert.Contains(t, other, "$(inherited)")
		assert.Contains(t, other, "-fdepscan-prefix-map=/work/app=/^src")
		assert.Contains(t, other, "/^dd")
		assert.Contains(t, other, "/^obj")
	})

	t.Run("respects user-supplied DerivedDataPath (no injection)", func(t *testing.T) {
		argsMock := &xcodeargsMocks.XcodeArgsMock{
			ArgsFunc:            func(_ map[string]string) []string { return []string{"xcodebuild"} },
			ProjectDirFunc:      func() string { return "/work/app" },
			DerivedDataPathFunc: func() string { return "/user/dd" },
			ProjectTempDirFunc:  func() string { return "/user/ptd" },
			UserOtherCFlagsFunc: func() string { return "" },
			CommandFunc:         func() string { return "xcodebuild" },
			ShortCommandFunc:    func() string { return "xcodebuild" },
		}
		var captured []string
		r := newRunnerWithArgs(xcelerate.Config{
			BuildCacheEnabled: true,
			ProxySocketPath:   "/tmp/proxy.sock",
		}, argsMock, &captured)

		_ = r.Run(context.Background())

		assert.NotContains(t, captured, xcodeargs.DerivedDataPathFlag, "user's -derivedDataPath must be left alone")

		other := findBuildSetting(captured, xcodeargs.OtherCFlagsKey)
		assert.Contains(t, other, "-fdepscan-prefix-map=/user/dd=/^dd")
		assert.Contains(t, other, "-fdepscan-prefix-map=/user/ptd=/^obj")
	})

	t.Run("splices user OTHER_CFLAGS between $(inherited) and prefix-map suffix", func(t *testing.T) {
		argsMock := &xcodeargsMocks.XcodeArgsMock{
			ArgsFunc: func(_ map[string]string) []string {
				return []string{"xcodebuild", "OTHER_CFLAGS=$(inherited) -Werror"}
			},
			ProjectDirFunc:      func() string { return "/work/app" },
			DerivedDataPathFunc: func() string { return "" },
			ProjectTempDirFunc:  func() string { return "" },
			UserOtherCFlagsFunc: func() string { return "$(inherited) -Werror" },
			CommandFunc:         func() string { return "xcodebuild" },
			ShortCommandFunc:    func() string { return "xcodebuild" },
		}
		var captured []string
		r := newRunnerWithArgs(xcelerate.Config{
			BuildCacheEnabled: true,
			ProxySocketPath:   "/tmp/proxy.sock",
		}, argsMock, &captured)

		_ = r.Run(context.Background())

		other := findBuildSetting(captured, xcodeargs.OtherCFlagsKey)
		assert.Regexp(t, `^\$\(inherited\) -Werror -fdepscan-prefix-map=`, other)
	})

	t.Run("--no-prefix-map opt-out disables the entire mechanism", func(t *testing.T) {
		argsMock := &xcodeargsMocks.XcodeArgsMock{
			ArgsFunc:         func(_ map[string]string) []string { return []string{"xcodebuild"} },
			ProjectDirFunc:   func() string { return "/work/app" },
			CommandFunc:      func() string { return "xcodebuild" },
			ShortCommandFunc: func() string { return "xcodebuild" },
		}
		var captured []string
		r := newRunnerWithArgs(xcelerate.Config{
			BuildCacheEnabled: true,
			ProxySocketPath:   "/tmp/proxy.sock",
		}, argsMock, &captured, func(rr *xcode.XcodebuildRunner) { rr.NoPrefixMap = true })

		_ = r.Run(context.Background())

		assert.NotContains(t, captured, xcodeargs.DerivedDataPathFlag)
		assert.Empty(t, findBuildSetting(captured, xcodeargs.OtherCFlagsKey))
	})

	t.Run("--no-managed-derived-data leaves prefix mapping on but skips the wrapper-owned dirs", func(t *testing.T) {
		var receivedAdditional map[string]string
		argsMock := &xcodeargsMocks.XcodeArgsMock{
			ArgsFunc: func(additional map[string]string) []string {
				receivedAdditional = additional

				return []string{"xcodebuild"}
			},
			ProjectDirFunc:      func() string { return "/work/app" },
			DerivedDataPathFunc: func() string { return "" },
			ProjectTempDirFunc:  func() string { return "" },
			UserOtherCFlagsFunc: func() string { return "" },
			CommandFunc:         func() string { return "xcodebuild" },
			ShortCommandFunc:    func() string { return "xcodebuild" },
		}
		var captured []string
		r := newRunnerWithArgs(xcelerate.Config{
			BuildCacheEnabled: true,
			ProxySocketPath:   "/tmp/proxy.sock",
		}, argsMock, &captured, func(rr *xcode.XcodebuildRunner) { rr.NoManagedDD = true })

		_ = r.Run(context.Background())

		assert.Equal(t, "YES", receivedAdditional[xcodeargs.ClangEnablePrefixMappingKey])
		assert.NotContains(t, receivedAdditional, xcodeargs.ProjectTempDirKey, "PROJECT_TEMP_DIR must not be injected when managed-dd is disabled")
		assert.NotContains(t, captured, xcodeargs.DerivedDataPathFlag)

		// $(inherited) still emitted for the home-only prefix rule.
		other := findBuildSetting(captured, xcodeargs.OtherCFlagsKey)
		assert.Contains(t, other, "-fdepscan-prefix-map=/work/app=/^src")
		assert.NotContains(t, other, "/^dd")
		assert.NotContains(t, other, "/^obj")
	})
}

func indexOf(argv []string, want string) int {
	for i, a := range argv {
		if a == want {
			return i
		}
	}

	return -1
}

func findBuildSetting(argv []string, key string) string { //nolint:unparam // key kept as arg to document intent even though only one setting is asserted today
	prefix := key + "="
	var last string
	for _, a := range argv {
		if len(a) > len(prefix) && a[:len(prefix)] == prefix {
			last = a[len(prefix):]
		}
	}

	return last
}
