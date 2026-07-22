//go:build unit

package xcode

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/analytics/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
)

func Test_workspaceSHA_deterministic(t *testing.T) {
	a := workspaceSHA("/work/app")
	b := workspaceSHA("/work/app")

	assert.Equal(t, a, b, "same input must produce the same output")
}

func Test_workspaceSHA_differentInputs(t *testing.T) {
	a := workspaceSHA("/work/app")
	b := workspaceSHA("/work/other")

	assert.NotEqual(t, a, b)
}

func Test_workspaceSHA_emptyInput(t *testing.T) {
	// Empty input must be stable and non-panic; just assert determinism.
	a := workspaceSHA("")
	b := workspaceSHA("")

	assert.Equal(t, a, b)
	assert.NotEmpty(t, a, "sha256 of empty produces a non-empty hex string")
}

func Test_workspaceSHA_hexShape(t *testing.T) {
	got := workspaceSHA("/work/app")

	// Code takes the first 8 bytes of sha256 and hex-encodes → 16 lowercase hex chars.
	assert.Len(t, got, 16)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]+$`), got)
}

func newRunnerForResolveTest(argsMock *xcodeargsMocks.XcodeArgsMock, home string, noManagedDD bool) *XcodebuildRunner {
	return &XcodebuildRunner{
		Config:      xcelerate.Config{},
		Metadata:    common.CacheConfigMetadata{},
		Logger:      bundleTestLogger,
		CacheLogger: bundleTestLogger,
		XcodeArgs:   argsMock,
		NoManagedDD: noManagedDD,
		Paths:       paths.FromHome(home),
	}
}

func Test_XcodebuildRunner_resolvePrefixMapPaths_userSuppliedWins(t *testing.T) {
	argsMock := &xcodeargsMocks.XcodeArgsMock{
		ProjectDirFunc:      func() string { return "/work/app" },
		DerivedDataPathFunc: func() string { return "/user/dd" },
		ProjectTempDirFunc:  func() string { return "/user/ptd" },
	}
	r := newRunnerForResolveTest(argsMock, "/h", false)

	got, _ := r.resolvePrefixMapPaths()

	assert.Equal(t, "/work/app", got.ProjectDir)
	assert.Equal(t, "/user/dd", got.DerivedDataPath, "user-supplied DerivedDataPath wins")
	assert.Equal(t, "/user/ptd", got.ProjectTempDir, "user-supplied ProjectTempDir wins")
	// Home comes from os.UserHomeDir(); assert non-empty rather than fighting the OS.
	assert.NotEmpty(t, got.Home)
}

func Test_XcodebuildRunner_resolvePrefixMapPaths_wrapperOwnedWhenUserBlank(t *testing.T) {
	argsMock := &xcodeargsMocks.XcodeArgsMock{
		ProjectDirFunc:      func() string { return "/work/app" },
		DerivedDataPathFunc: func() string { return "" },
		ProjectTempDirFunc:  func() string { return "" },
	}
	r := newRunnerForResolveTest(argsMock, "/h", false)

	got, _ := r.resolvePrefixMapPaths()

	assert.Equal(t, "/work/app", got.ProjectDir)
	sha := workspaceSHA("/work/app")
	assert.Contains(t, got.DerivedDataPath, "/h/.bitrise/cache/xcode-dd/"+sha)
	assert.Contains(t, got.ProjectTempDir, "/h/.bitrise/cache/xcode-ptd/"+sha)
}

func Test_XcodebuildRunner_resolvePrefixMapPaths_noManagedDDSkipsWrapperOwned(t *testing.T) {
	argsMock := &xcodeargsMocks.XcodeArgsMock{
		ProjectDirFunc:      func() string { return "/work/app" },
		DerivedDataPathFunc: func() string { return "" },
		ProjectTempDirFunc:  func() string { return "" },
	}
	r := newRunnerForResolveTest(argsMock, "/h", true)

	got, _ := r.resolvePrefixMapPaths()

	assert.Equal(t, "/work/app", got.ProjectDir)
	assert.Empty(t, got.DerivedDataPath, "NoManagedDD must skip wrapper-owned DerivedDataPath")
	assert.Empty(t, got.ProjectTempDir, "NoManagedDD must skip wrapper-owned ProjectTempDir")
}

func Test_XcodebuildRunner_resolvePrefixMapPaths_emptyProjectDirSkipsWrapperOwned(t *testing.T) {
	argsMock := &xcodeargsMocks.XcodeArgsMock{
		ProjectDirFunc:      func() string { return "" },
		DerivedDataPathFunc: func() string { return "" },
		ProjectTempDirFunc:  func() string { return "" },
	}
	r := newRunnerForResolveTest(argsMock, "/h", false)

	got, _ := r.resolvePrefixMapPaths()

	assert.Empty(t, got.ProjectDir)
	assert.Empty(t, got.DerivedDataPath, "no project dir means no workspace SHA, so no managed dirs")
	assert.Empty(t, got.ProjectTempDir)
}

func Test_XcodebuildRunner_resolvePrefixMapPaths_sourcesTracked(t *testing.T) {
	argsMock := &xcodeargsMocks.XcodeArgsMock{
		ProjectDirFunc:      func() string { return "/work/app" },
		DerivedDataPathFunc: func() string { return "/user/dd" },
		ProjectTempDirFunc:  func() string { return "" },
	}
	r := newRunnerForResolveTest(argsMock, "/h", false)

	_, sources := r.resolvePrefixMapPaths()

	assert.Equal(t, prefixMapSourceAuto, sources.Home)
	assert.Equal(t, prefixMapSourceArgv, sources.ProjectDir)
	assert.Equal(t, prefixMapSourceArgv, sources.DerivedDataPath, "user-supplied DD wins as argv")
	assert.Equal(t, prefixMapSourceManaged, sources.ProjectTempDir, "blank PTD falls to managed")
}

func Test_XcodebuildRunner_resolvePaths_returnsInjectedPathsWhenSet(t *testing.T) {
	r := &XcodebuildRunner{
		Logger: bundleTestLogger,
		Paths:  paths.FromHome("/injected/home"),
	}

	got := r.resolvePaths()
	assert.Equal(t, "/injected/home", got.Home)
}

func Test_XcodebuildRunner_resolvePaths_fallsBackToDefault(t *testing.T) {
	r := &XcodebuildRunner{
		Logger: bundleTestLogger,
		// Paths.Home is empty → falls back to paths.Default().
	}

	// paths.Default() reads $HOME.
	t.Setenv("HOME", "/tmp/fallback-home")

	got := r.resolvePaths()
	assert.Equal(t, "/tmp/fallback-home", got.Home)
}

func Test_replaceOrAppendBuildSetting_appendsWhenAbsent(t *testing.T) {
	argv := []string{"xcodebuild", "-scheme", "App"}

	out := replaceOrAppendBuildSetting(argv, "OTHER_CFLAGS", "-Wall")

	require.Equal(t, []string{"xcodebuild", "-scheme", "App", "OTHER_CFLAGS=-Wall"}, out)
}

func Test_replaceOrAppendBuildSetting_replacesSingleOccurrence(t *testing.T) {
	argv := []string{"xcodebuild", "OTHER_CFLAGS=$(inherited) -Werror", "-scheme", "App"}

	out := replaceOrAppendBuildSetting(argv, "OTHER_CFLAGS", "-Wall")

	require.Equal(t, []string{"xcodebuild", "OTHER_CFLAGS=-Wall", "-scheme", "App"}, out)
}

func Test_replaceOrAppendBuildSetting_multipleOccurrences_keepsFirstDropsRest(t *testing.T) {
	// Reading the code: first hit is rewritten in place and marks replaced=true;
	// subsequent hits are dropped ("continue"); no re-append at the end.
	argv := []string{
		"OTHER_CFLAGS=old-1",
		"-scheme", "App",
		"OTHER_CFLAGS=old-2",
	}

	out := replaceOrAppendBuildSetting(argv, "OTHER_CFLAGS", "new")

	require.Equal(t, []string{"OTHER_CFLAGS=new", "-scheme", "App"}, out,
		"first occurrence gets replaced in place; later occurrences are dropped")
}

func Test_replaceOrAppendBuildSetting_emptyArgv(t *testing.T) {
	out := replaceOrAppendBuildSetting(nil, "KEY", "value")

	require.Equal(t, []string{"KEY=value"}, out)
}

func Test_replaceOrAppendBuildSetting_valuePreservesEmbeddedEquals(t *testing.T) {
	argv := []string{"xcodebuild"}

	out := replaceOrAppendBuildSetting(argv, "KEY", "a=b=c")

	require.Equal(t, []string{"xcodebuild", "KEY=a=b=c"}, out,
		"only the first '=' separates key from value; the rest is preserved")
}

func Test_XcodebuildRunner_assembleArgs_skipsInjectionForNonBuildActions(t *testing.T) {
	argv := []string{"-list", "-json", "-project", "foo.xcodeproj"}
	argsMock := &xcodeargsMocks.XcodeArgsMock{
		HasBuildActionFunc: func() bool { return false },
		ArgsFunc: func(_ map[string]string) []string {
			return argv
		},
	}
	r := &XcodebuildRunner{
		Config:      xcelerate.Config{BuildCacheEnabled: true, ProxySocketPath: "/tmp/sock"},
		Metadata:    common.CacheConfigMetadata{},
		Logger:      bundleTestLogger,
		CacheLogger: bundleTestLogger,
		XcodeArgs:   argsMock,
	}

	out := r.assembleArgs()

	require.Equal(t, argv, out, "non-build argv must pass through unchanged")
	require.Len(t, argsMock.ArgsCalls(), 1)
	require.Empty(t, argsMock.ArgsCalls()[0].Additional,
		"no wrapper additions when there's no build action")
}

func Test_XcodebuildRunner_assembleArgs_buildActionInjectsProxySocket(t *testing.T) {
	argsMock := &xcodeargsMocks.XcodeArgsMock{
		HasBuildActionFunc:  func() bool { return true },
		ProjectDirFunc:      func() string { return "" },
		DerivedDataPathFunc: func() string { return "" },
		ProjectTempDirFunc:  func() string { return "" },
		UserOtherCFlagsFunc: func() string { return "" },
		ArgsFunc: func(_ map[string]string) []string {
			return []string{}
		},
	}
	r := &XcodebuildRunner{
		Config: xcelerate.Config{
			BuildCacheEnabled:    true,
			ProxySocketPath:      "/tmp/sock",
			DisablePrefixMapping: true,
			BuildCacheSkipFlags:  true,
		},
		Metadata:    common.CacheConfigMetadata{},
		Logger:      bundleTestLogger,
		CacheLogger: bundleTestLogger,
		XcodeArgs:   argsMock,
	}

	_ = r.assembleArgs()

	require.Len(t, argsMock.ArgsCalls(), 1)
	additional := argsMock.ArgsCalls()[0].Additional
	require.Equal(t, "/tmp/sock", additional["COMPILATION_CACHE_REMOTE_SERVICE_PATH"],
		"build-action path still receives the proxy socket wiring")
}

type countingSessionClient struct {
	setCalls atomic.Int32
}

func (c *countingSessionClient) SetSession(_ context.Context, _ *session.SetSessionRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	c.setCalls.Add(1)

	return &emptypb.Empty{}, nil
}

func (c *countingSessionClient) GetSessionStats(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*session.GetSessionStatsResponse, error) {
	return &session.GetSessionStatsResponse{}, nil
}

func (c *countingSessionClient) EndSession(_ context.Context, _ *session.EndSessionRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

type countingInvocationSaver struct {
	putCalls atomic.Int32
}

func (s *countingInvocationSaver) PutInvocation(_ analytics.Invocation) error {
	s.putCalls.Add(1)

	return nil
}

type recordingXcodeRunner struct {
	stats    xcodeargs.RunStats
	lastArgs []string
}

func (r *recordingXcodeRunner) Run(_ context.Context, args []string) xcodeargs.RunStats {
	r.lastArgs = append([]string(nil), args...)

	return r.stats
}

func Test_Run_QueryAction_PassthroughSkipsSetSessionAndAnalytics(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	invID := "query-inv-1"
	origArgs := []string{"-list", "-json", "-project", "foo.xcodeproj"}

	argsMock := &xcodeargsMocks.XcodeArgsMock{
		HasBuildActionFunc: func() bool { return false },
		ArgsFunc: func(additional map[string]string) []string {
			if len(additional) != 0 {
				t.Fatalf("expected no wrapper additions, got %v", additional)
			}

			return origArgs
		},
	}

	xcodeRunner := &recordingXcodeRunner{stats: xcodeargs.RunStats{ExitCode: 42, Success: false}}
	sessionClient := &countingSessionClient{}
	invocationAPI := &countingInvocationSaver{}
	relationAPI := &relationSenderMock{
		PutInvocationRelationFunc: func(_ multiplatform.InvocationRelation) error { return nil },
	}
	localLogger := &localInvocationLoggerMock{
		AppendFunc: func(_ invocations.Record) error { return nil },
	}

	r := &XcodebuildRunner{
		Config:             xcelerate.Config{BuildCacheEnabled: true, ProxySocketPath: "/tmp/sock"},
		Metadata:           common.CacheConfigMetadata{},
		InvocationID:       invID,
		Logger:             bundleTestLogger,
		CacheLogger:        bundleTestLogger,
		XcodeRunner:        xcodeRunner,
		ProxySessionClient: sessionClient,
		XcodeArgs:          argsMock,
		invocationAPI:      invocationAPI,
		relationAPI:        relationAPI,
		localLogger:        localLogger,
	}

	stats := r.Run(context.Background())

	assert.Equal(t, 42, stats.ExitCode, "wrapper must return xcodebuild exit code as-is")
	assert.Equal(t, origArgs, xcodeRunner.lastArgs, "xcodebuild must receive the original argv unchanged")
	assert.Zero(t, sessionClient.setCalls.Load(), "SetSession must not fire for query invocations")
	assert.Zero(t, invocationAPI.putCalls.Load(), "PutInvocation must not fire for query invocations")
	assert.Empty(t, relationAPI.PutInvocationRelationCalls(), "PutInvocationRelation must not fire for query invocations")
	assert.Empty(t, localLogger.AppendCalls(), "local invocation log must not be written for query invocations")

	marker := filepath.Join(home, ".local", "state", "xcelerate", "enrichment", "handled-invocations", invID)
	_, err := os.Stat(marker)
	assert.True(t, os.IsNotExist(err), "handled-invocation marker must not be written for query invocations")
}

func Test_Run_BuildAction_StillEmitsAnalyticsAndSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BITRISE_INVOCATION_ID", "")

	argsMock := &xcodeargsMocks.XcodeArgsMock{
		HasBuildActionFunc: func() bool { return true },
		ArgsFunc:           func(_ map[string]string) []string { return []string{"xcodebuild"} },
		CommandFunc:        func() string { return "xcodebuild -scheme App" },
		ShortCommandFunc:   func() string { return "xcodebuild build" },
	}

	xcodeRunner := &recordingXcodeRunner{stats: xcodeargs.RunStats{Success: true}}
	sessionClient := &countingSessionClient{}
	invocationAPI := &countingInvocationSaver{}
	relationAPI := &relationSenderMock{
		PutInvocationRelationFunc: func(_ multiplatform.InvocationRelation) error { return nil },
	}
	localLogger := &localInvocationLoggerMock{
		AppendFunc: func(_ invocations.Record) error { return nil },
	}

	r := &XcodebuildRunner{
		Config:             xcelerate.Config{BuildCacheEnabled: true, ProxySocketPath: "/tmp/sock", Silent: true},
		Metadata:           common.CacheConfigMetadata{},
		InvocationID:       "build-inv-1",
		Logger:             bundleTestLogger,
		CacheLogger:        bundleTestLogger,
		XcodeRunner:        xcodeRunner,
		ProxySessionClient: sessionClient,
		XcodeArgs:          argsMock,
		invocationAPI:      invocationAPI,
		relationAPI:        relationAPI,
		localLogger:        localLogger,
	}

	_ = r.Run(context.Background())

	assert.Equal(t, int32(1), sessionClient.setCalls.Load(), "SetSession must fire on the build path")
	assert.Equal(t, int32(1), invocationAPI.putCalls.Load(), "PutInvocation must fire on the build path")
	assert.Len(t, localLogger.AppendCalls(), 1, "local invocation log must be written on the build path")

	marker := filepath.Join(home, ".local", "state", "xcelerate", "enrichment", "handled-invocations", "build-inv-1")
	_, err := os.Stat(marker)
	assert.NoError(t, err, "handled-invocation marker must be written on the build path")
}

