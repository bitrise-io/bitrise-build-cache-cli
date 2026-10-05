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
	// <developer>/usr/bin/xcodebuild shape → derives developer dir without xcode-select.
	fakeXcodebuild := filepath.Join(fakeDeveloperDir, "usr", "bin", "xcodebuild")

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
	}

	installXcodeToolchain(context.Background(), log.NewLogger(), osProxy, failingCmdFunc(t),
		"/tmp/proxy.sock", fakeXcodebuild)

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

	// Non-matching shape forces the xcode-select fallback.
	installXcodeToolchain(context.Background(), log.NewLogger(), osProxy, cmdFunc,
		"/tmp/proxy.sock", "/opt/xcode-wrapper/xcodebuild")

	assert.True(t, called, "xcode-select fallback should run when the xcodebuild path shape doesn't match")

	p := paths.FromHome(home)
	assert.FileExists(t, filepath.Join(p.XcodeToolchainBundleDir(), "ToolchainInfo.plist"))
}

func TestInstallXcodeToolchain_isNonFatalOnBrokenDeveloperDir(t *testing.T) {
	skipIfNonUnix(t)

	home := t.TempDir()
	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
	}

	cmdFunc := func(_ context.Context, _ string, _ ...string) utils.Command {
		return &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return nil, errors.New("xcode-select missing")
			},
		}
	}

	installXcodeToolchain(context.Background(), log.NewLogger(), osProxy, cmdFunc,
		"/tmp/proxy.sock", "/opt/not-matching/path")

	_, err := os.Stat(paths.FromHome(home).XcodeToolchainBundleDir())
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestUninstallXcodeToolchain_removesBundleAndSymlink(t *testing.T) {
	skipIfNonUnix(t)

	home := t.TempDir()
	fakeDeveloperDir := filepath.Join(t.TempDir(), "Developer")
	seedFakeDeveloperDir(t, fakeDeveloperDir)
	fakeXcodebuild := filepath.Join(fakeDeveloperDir, "usr", "bin", "xcodebuild")

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
	}

	installXcodeToolchain(context.Background(), log.NewLogger(), osProxy, failingCmdFunc(t),
		"/tmp/proxy.sock", fakeXcodebuild)
	uninstallXcodeToolchain(log.NewLogger(), osProxy)

	p := paths.FromHome(home)
	_, err := os.Lstat(p.XcodeToolchainBundleDir())
	assert.True(t, errors.Is(err, fs.ErrNotExist))
	_, err = os.Lstat(p.XcodeToolchainsLinkPath())
	assert.True(t, errors.Is(err, fs.ErrNotExist))
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
