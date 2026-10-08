//go:build unit

package xcode_app

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderToolchainInfoPlist_containsAllRequiredKeys(t *testing.T) {
	body, err := RenderToolchainInfoPlist("/tmp/proxy.sock", "/Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib")
	require.NoError(t, err)

	rendered := string(body)
	for _, want := range []string{
		"<key>Identifier</key>",
		"<string>" + ToolchainID + "</string>",
		"<key>DisplayName</key>",
		"<string>" + ToolchainDisplayName + "</string>",
		"<key>CompatibilityVersion</key>",
		"<integer>2</integer>",
		"<key>CompatibilityVersionDisplayString</key>",
		"<key>OverrideBuildSettings</key>",
		"<key>CLANG_ENABLE_COMPILE_CACHE</key>",
		"<key>CLANG_ENABLE_MODULES</key>",
		"<key>COMPILATION_CACHE_ENABLE_CACHING</key>",
		"<key>COMPILATION_CACHE_ENABLE_PLUGIN</key>",
		"<key>COMPILATION_CACHE_PLUGIN_PATH</key>",
		"<string>/Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib</string>",
		"<key>COMPILATION_CACHE_REMOTE_SERVICE_PATH</key>",
		"<string>/tmp/proxy.sock</string>",
		"<key>OTHER_SWIFT_FLAGS</key>",
		"<string>$(inherited) -cas-plugin-option remote-service-path=/tmp/proxy.sock</string>",
		"<key>SWIFT_ENABLE_COMPILE_CACHE</key>",
	} {
		assert.Contains(t, rendered, want)
	}
}

func TestRenderToolchainInfoPlist_isValidXML(t *testing.T) {
	body, err := RenderToolchainInfoPlist("/tmp/proxy.sock", "/dev/null")
	require.NoError(t, err)

	// Well-formed XML is the minimum; full plist decode isn't portable.
	decoder := xml.NewDecoder(bytesReader(body))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
}

func TestRenderToolchainInfoPlist_emptyInputsAreErrors(t *testing.T) {
	_, err := RenderToolchainInfoPlist("", "/dev/null")
	require.Error(t, err)

	_, err = RenderToolchainInfoPlist("/tmp/x.sock", "")
	require.Error(t, err)
}

func TestInstallToolchain_producesExpectedBundleLayout(t *testing.T) {
	skipIfNonUnix(t)

	tmp := t.TempDir()
	defaultTC := filepath.Join(tmp, "XcodeDefault.xctoolchain")
	installPath := filepath.Join(tmp, "install", ToolchainID)
	seedFakeDefaultToolchain(t, defaultTC)

	require.NoError(t, InstallToolchain(installPath, defaultTC, "/tmp/proxy.sock", "/Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib", ""))

	plist := filepath.Join(installPath, toolchainInfoPlistFile)
	plistBody, err := os.ReadFile(plist) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(plistBody), "<key>Identifier</key>")

	for _, name := range []string{"swiftc", "clang"} {
		linkPath := filepath.Join(installPath, "usr", "bin", name)
		target, err := os.Readlink(linkPath)
		require.NoError(t, err, "symlink %s missing", linkPath)
		assert.Equal(t, filepath.Join(defaultTC, "usr", "bin", name), target)
	}

	// libexec not in the seed; must not be synthesised.
	libLink := filepath.Join(installPath, "usr", "lib")
	target, err := os.Readlink(libLink)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(defaultTC, "usr", "lib"), target)
	_, err = os.Lstat(filepath.Join(installPath, "usr", "libexec"))
	assert.True(t, errors.Is(err, fs.ErrNotExist))

	devLink := filepath.Join(installPath, "Developer")
	target, err = os.Readlink(devLink)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(defaultTC, "Developer"), target)
}

func TestInstallToolchain_isIdempotent(t *testing.T) {
	skipIfNonUnix(t)

	tmp := t.TempDir()
	defaultTC := filepath.Join(tmp, "XcodeDefault.xctoolchain")
	installPath := filepath.Join(tmp, "install", ToolchainID)
	seedFakeDefaultToolchain(t, defaultTC)

	require.NoError(t, InstallToolchain(installPath, defaultTC, "/tmp/p1.sock", "/dev/null", ""))
	require.NoError(t, InstallToolchain(installPath, defaultTC, "/tmp/p2.sock", "/dev/null", ""))

	body, err := os.ReadFile(filepath.Join(installPath, toolchainInfoPlistFile)) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(body), "<string>/tmp/p2.sock</string>")
	assert.NotContains(t, string(body), "/tmp/p1.sock")
}

func TestLinkToolchain_replacesStalePointer(t *testing.T) {
	skipIfNonUnix(t)

	tmp := t.TempDir()
	first := filepath.Join(tmp, "first")
	second := filepath.Join(tmp, "second")
	linkPath := filepath.Join(tmp, "linkdir", ToolchainID)
	require.NoError(t, os.MkdirAll(first, 0o755))
	require.NoError(t, os.MkdirAll(second, 0o755))

	require.NoError(t, LinkToolchain(first, linkPath))
	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, first, target)

	require.NoError(t, LinkToolchain(second, linkPath))
	target, err = os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, second, target)
}

func TestUninstallToolchain_removesLinkAndBundle(t *testing.T) {
	skipIfNonUnix(t)

	tmp := t.TempDir()
	defaultTC := filepath.Join(tmp, "XcodeDefault.xctoolchain")
	installPath := filepath.Join(tmp, "install", ToolchainID)
	linkPath := filepath.Join(tmp, "linkdir", ToolchainID)
	seedFakeDefaultToolchain(t, defaultTC)

	require.NoError(t, InstallToolchain(installPath, defaultTC, "/tmp/p.sock", "/dev/null", ""))
	require.NoError(t, LinkToolchain(installPath, linkPath))

	require.NoError(t, UninstallToolchain(installPath, linkPath))

	_, err := os.Lstat(installPath)
	assert.True(t, errors.Is(err, fs.ErrNotExist), "install path should be gone")
	_, err = os.Lstat(linkPath)
	assert.True(t, errors.Is(err, fs.ErrNotExist), "link should be gone")
}

func TestUninstallToolchain_leavesForeignLinkUntouched(t *testing.T) {
	skipIfNonUnix(t)

	tmp := t.TempDir()
	installPath := filepath.Join(tmp, "ours")
	otherTarget := filepath.Join(tmp, "someone-elses")
	linkPath := filepath.Join(tmp, "linkdir", ToolchainID)
	require.NoError(t, os.MkdirAll(installPath, 0o755))
	require.NoError(t, os.MkdirAll(otherTarget, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(linkPath), 0o755))
	// Foreign link (not pointing at our install path) must survive uninstall.
	require.NoError(t, os.Symlink(otherTarget, linkPath))

	require.NoError(t, UninstallToolchain(installPath, linkPath))

	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, otherTarget, target)
	_, err = os.Lstat(installPath)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestInstallToolchain_TrampolinedBinsAreCopiesOtherEntriesStaySymlinks(t *testing.T) {
	skipIfNonUnix(t)

	tmp := t.TempDir()
	defaultTC := filepath.Join(tmp, "XcodeDefault.xctoolchain")
	installPath := filepath.Join(tmp, "install", ToolchainID)
	seedFakeDefaultToolchainWithExtras(t, defaultTC, []string{"swiftc", "clang", "swift", "ld", "clang-stat-cache"})

	trampoline := filepath.Join(tmp, "trampoline")
	require.NoError(t, os.WriteFile(trampoline, []byte("TRAMPOLINE"), 0o755)) //nolint:gosec // test fixture

	require.NoError(t, InstallToolchain(installPath, defaultTC, "/tmp/proxy.sock", "/dev/null", trampoline))

	for _, name := range []string{"swiftc", "clang", "swift"} {
		p := filepath.Join(installPath, "usr", "bin", name)
		fi, err := os.Lstat(p)
		require.NoError(t, err)
		assert.Zero(t, fi.Mode()&os.ModeSymlink, "%s must be a trampoline copy, not a symlink", name)
		body, err := os.ReadFile(p) //nolint:gosec // test-controlled path
		require.NoError(t, err)
		assert.Equal(t, []byte("TRAMPOLINE"), body)
	}

	for _, name := range []string{"ld", "clang-stat-cache"} {
		p := filepath.Join(installPath, "usr", "bin", name)
		fi, err := os.Lstat(p)
		require.NoError(t, err)
		assert.NotZero(t, fi.Mode()&os.ModeSymlink, "%s must remain a symlink", name)
	}
}

func TestInstallToolchain_EmptyTrampolinePathPureSymlinkLayout(t *testing.T) {
	skipIfNonUnix(t)

	tmp := t.TempDir()
	defaultTC := filepath.Join(tmp, "XcodeDefault.xctoolchain")
	installPath := filepath.Join(tmp, "install", ToolchainID)
	seedFakeDefaultToolchain(t, defaultTC)

	require.NoError(t, InstallToolchain(installPath, defaultTC, "/tmp/proxy.sock", "/dev/null", ""))

	for _, name := range []string{"swiftc", "clang"} {
		p := filepath.Join(installPath, "usr", "bin", name)
		fi, err := os.Lstat(p)
		require.NoError(t, err)
		assert.NotZero(t, fi.Mode()&os.ModeSymlink, "%s must be a symlink when trampoline path is empty", name)
	}
}

func seedFakeDefaultToolchainWithExtras(t *testing.T, root string, bins []string) {
	t.Helper()

	for _, dir := range []string{"usr/bin", "usr/lib", "Developer"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
	}
	for _, bin := range bins {
		require.NoError(t, os.WriteFile(filepath.Join(root, "usr", "bin", bin), []byte("#!/bin/sh\nexit 0\n"), 0o755)) //nolint:gosec
	}
}

func seedFakeDefaultToolchain(t *testing.T, root string) {
	t.Helper()

	for _, dir := range []string{"usr/bin", "usr/lib", "Developer"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
	}
	for _, bin := range []string{"swiftc", "clang"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "usr", "bin", bin), []byte("#!/bin/sh\nexit 0\n"), 0o755)) //nolint:gosec
	}
}

func skipIfNonUnix(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("symlink tests are POSIX-only")
	}
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
