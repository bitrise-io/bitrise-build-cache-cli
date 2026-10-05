//go:build unit

package xcelerate

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func TestInstallXcodeToolchain_endToEnd(t *testing.T) {
	skipIfNonUnix(t)

	home := t.TempDir()
	fakeDeveloperDir := filepath.Join(t.TempDir(), "Developer")
	seedFakeDeveloperDir(t, fakeDeveloperDir)
	// <developer>/usr/bin/xcodebuild shape → devdir.FromXcodebuildPath resolves without xcode-select.
	fakeXcodebuild := filepath.Join(fakeDeveloperDir, "usr", "bin", "xcodebuild")

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		LstatFunc:       anchorMissing,
	}

	installXcodeToolchain(context.Background(), log.NewLogger(), osProxy, failingCmdFunc(t),
		nil, "/tmp/proxy.sock", fakeXcodebuild)

	p := paths.FromHome(home)
	assert.FileExists(t, filepath.Join(p.XcodeToolchainBundleDir(), "ToolchainInfo.plist"))
	target, err := os.Readlink(p.XcodeToolchainsLinkPath())
	require.NoError(t, err)
	assert.Equal(t, p.XcodeToolchainBundleDir(), target)
}

func TestInstallXcodeToolchain_fallsBackToXcodeSelect(t *testing.T) {
	skipIfNonUnix(t)

	home := t.TempDir()
	fakeDeveloperDir := filepath.Join(t.TempDir(), "Developer")
	seedFakeDeveloperDir(t, fakeDeveloperDir)

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		LstatFunc:       anchorMissing,
	}

	called := false
	cmdFunc := func(_ context.Context, cmd string, args ...string) utils.Command {
		if cmd == "xcode-select" && len(args) > 0 && args[0] == "-p" {
			called = true

			return &utilsMocks.CommandMock{
				CombinedOutputFunc: func() ([]byte, error) {
					return []byte(fakeDeveloperDir + "\n"), nil
				},
			}
		}

		t.Fatalf("unexpected command %s %v", cmd, args)

		return nil
	}

	// Non-matching shape → devdir.FromXcodebuildPath returns "" and xcode-select fallback kicks in.
	installXcodeToolchain(context.Background(), log.NewLogger(), osProxy, cmdFunc,
		nil, "/tmp/proxy.sock", "/opt/xcode-wrapper/xcodebuild")

	assert.True(t, called, "xcode-select fallback should run when the xcodebuild path shape doesn't match")

	p := paths.FromHome(home)
	assert.FileExists(t, filepath.Join(p.XcodeToolchainBundleDir(), "ToolchainInfo.plist"))
}

func TestInstallXcodeToolchain_isNonFatalOnBrokenDeveloperDir(t *testing.T) {
	skipIfNonUnix(t)

	home := t.TempDir()
	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		LstatFunc:       anchorMissing,
	}

	cmdFunc := func(_ context.Context, _ string, _ ...string) utils.Command {
		return &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return nil, errors.New("xcode-select missing")
			},
		}
	}

	installXcodeToolchain(context.Background(), log.NewLogger(), osProxy, cmdFunc,
		nil, "/tmp/proxy.sock", "/opt/not-matching/path")

	_, err := os.Stat(paths.FromHome(home).XcodeToolchainBundleDir())
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestInstallXcodeToolchain_pluginPathAnchoredWhenAnchorLive(t *testing.T) {
	skipIfNonUnix(t)

	home := t.TempDir()
	xcodeA := filepath.Join(t.TempDir(), "xcode-a", "Developer")
	seedFakeDeveloperDir(t, xcodeA)
	fakeXcodebuild := filepath.Join(xcodeA, "usr", "bin", "xcodebuild")

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		LstatFunc:       anchorPresent,
	}

	installXcodeToolchain(context.Background(), log.NewLogger(), osProxy, failingCmdFunc(t),
		nil, "/tmp/proxy.sock", fakeXcodebuild)

	body := readToolchainPlist(t, home)
	assert.Contains(t, body, "<string>/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib</string>",
		"plist should carry anchor-rooted plugin path when anchor is live")
	assert.NotContains(t, body, filepath.Join(xcodeA, "usr", "lib", "libToolchainCASPlugin.dylib"),
		"anchor-live branch must not fall back to the developer-dir path")
}

func TestInstallXcodeToolchain_pluginPathDerivedWhenAnchorAbsent(t *testing.T) {
	skipIfNonUnix(t)

	home := t.TempDir()
	xcodeA := filepath.Join(t.TempDir(), "xcode-a", "Developer")
	seedFakeDeveloperDir(t, xcodeA)
	fakeXcodebuild := filepath.Join(xcodeA, "usr", "bin", "xcodebuild")

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		LstatFunc:       anchorMissing,
	}

	installXcodeToolchain(context.Background(), log.NewLogger(), osProxy, failingCmdFunc(t),
		nil, "/tmp/proxy.sock", fakeXcodebuild)

	body := readToolchainPlist(t, home)
	derived := filepath.Join(xcodeA, "usr", "lib", "libToolchainCASPlugin.dylib")
	assert.Contains(t, body, "<string>"+derived+"</string>",
		"plist should carry developer-dir-derived plugin path when anchor is absent")
	assert.NotContains(t, body, "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib")
}

func TestInstallXcodeToolchain_pluginPathEnvOverrideBeatsAnchor(t *testing.T) {
	skipIfNonUnix(t)

	home := t.TempDir()
	xcodeA := filepath.Join(t.TempDir(), "xcode-a", "Developer")
	seedFakeDeveloperDir(t, xcodeA)
	fakeXcodebuild := filepath.Join(xcodeA, "usr", "bin", "xcodebuild")

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		LstatFunc:       anchorPresent,
	}

	envs := map[string]string{EnvCASPluginPath: "/opt/custom/libToolchainCASPlugin.dylib"}
	installXcodeToolchain(context.Background(), log.NewLogger(), osProxy, failingCmdFunc(t),
		envs, "/tmp/proxy.sock", fakeXcodebuild)

	body := readToolchainPlist(t, home)
	assert.Contains(t, body, "<string>/opt/custom/libToolchainCASPlugin.dylib</string>",
		"env override must beat the live anchor")
	assert.NotContains(t, body, "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib")
}

func TestUninstallXcodeToolchain_removesBundleAndSymlink(t *testing.T) {
	skipIfNonUnix(t)

	home := t.TempDir()
	fakeDeveloperDir := filepath.Join(t.TempDir(), "Developer")
	seedFakeDeveloperDir(t, fakeDeveloperDir)
	fakeXcodebuild := filepath.Join(fakeDeveloperDir, "usr", "bin", "xcodebuild")

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		LstatFunc:       anchorMissing,
	}

	installXcodeToolchain(context.Background(), log.NewLogger(), osProxy, failingCmdFunc(t),
		nil, "/tmp/proxy.sock", fakeXcodebuild)
	uninstallXcodeToolchain(log.NewLogger(), osProxy)

	p := paths.FromHome(home)
	_, err := os.Lstat(p.XcodeToolchainBundleDir())
	assert.True(t, errors.Is(err, fs.ErrNotExist))
	_, err = os.Lstat(p.XcodeToolchainsLinkPath())
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func seedFakeDeveloperDir(t *testing.T, root string) {
	t.Helper()

	defaultTC := filepath.Join(root, "Toolchains", "XcodeDefault.xctoolchain")
	for _, d := range []string{"usr/bin", "usr/lib", "Developer"} {
		require.NoError(t, os.MkdirAll(filepath.Join(defaultTC, d), 0o755))
	}
	for _, name := range []string{"swiftc", "clang"} {
		require.NoError(t, os.WriteFile(filepath.Join(defaultTC, "usr", "bin", name), []byte("#!/bin/sh\nexit 0\n"), 0o755)) //nolint:gosec
	}
}

func failingCmdFunc(t *testing.T) utils.CommandFunc {
	return func(_ context.Context, cmd string, args ...string) utils.Command {
		t.Fatalf("cmd should not be invoked (got %s %v)", cmd, args)

		return nil
	}
}

func skipIfNonUnix(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("symlink tests are POSIX-only")
	}
}

func readToolchainPlist(t *testing.T, home string) string {
	t.Helper()

	plistPath := filepath.Join(paths.FromHome(home).XcodeToolchainBundleDir(), "ToolchainInfo.plist")
	body, err := os.ReadFile(plistPath) //nolint:gosec // test-controlled path
	require.NoError(t, err)

	return string(body)
}

func TestDeveloperDirFromXcodebuildPath(t *testing.T) {
	cases := map[string]string{
		"/Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild": "/Applications/Xcode.app/Contents/Developer",
		"/opt/some-wrapper/xcodebuild": "",
		"":                             "",
	}
	for in, want := range cases {
		assert.Equal(t, want, developerDirFromXcodebuildPath(in), "input: %s", in)
	}
}

func TestResolveDeveloperDir_derivesFromXcodebuildPath(t *testing.T) {
	got, err := resolveDeveloperDir(context.Background(), failingCmdFunc(t),
		"/Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild")
	require.NoError(t, err)
	assert.Equal(t, "/Applications/Xcode.app/Contents/Developer", got)
}

func TestResolveDeveloperDir_fallsBackToXcodeSelect(t *testing.T) {
	called := false
	cmdFunc := func(_ context.Context, cmd string, args ...string) utils.Command {
		if cmd == "xcode-select" && len(args) > 0 && args[0] == "-p" {
			called = true

			return &utilsMocks.CommandMock{
				CombinedOutputFunc: func() ([]byte, error) {
					return []byte("/Applications/Xcode.app/Contents/Developer\n"), nil
				},
			}
		}

		t.Fatalf("unexpected command %s %v", cmd, args)

		return nil
	}

	got, err := resolveDeveloperDir(context.Background(), cmdFunc, "/opt/some-wrapper/xcodebuild")
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "/Applications/Xcode.app/Contents/Developer", got)
}

func TestResolveDeveloperDir_xcodeSelectErrorPropagates(t *testing.T) {
	cmdFunc := func(_ context.Context, _ string, _ ...string) utils.Command {
		return &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return nil, errors.New("xcode-select missing")
			},
		}
	}

	_, err := resolveDeveloperDir(context.Background(), cmdFunc, "/opt/not-matching/path")
	require.Error(t, err)
}

func TestResolveDeveloperDir_xcodeSelectEmptyIsError(t *testing.T) {
	cmdFunc := func(_ context.Context, _ string, _ ...string) utils.Command {
		return &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("\n"), nil
			},
		}
	}

	_, err := resolveDeveloperDir(context.Background(), cmdFunc, "/opt/not-matching/path")
	require.Error(t, err)
}
