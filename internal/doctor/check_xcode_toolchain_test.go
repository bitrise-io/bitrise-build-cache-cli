//go:build unit

package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

func TestDiagnoseXcodeToolchain_bundleMissingWarns(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "missing-bundle")

	res := diagnoseXcodeToolchain(bundlePath)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "toolchain bundle missing")
	assert.Contains(t, res.Detail, bundlePath)
}

func TestDiagnoseXcodeToolchain_plistMissingWarns(t *testing.T) {
	bundlePath := seedToolchainBundle(t, bundleOpts{skipPlist: true})

	res := diagnoseXcodeToolchain(bundlePath)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "Info.plist invalid")
}

func TestDiagnoseXcodeToolchain_plistWrongIdentifierWarns(t *testing.T) {
	bundlePath := seedToolchainBundle(t, bundleOpts{wrongIdentifier: true})

	res := diagnoseXcodeToolchain(bundlePath)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "Info.plist invalid")
}

func TestDiagnoseXcodeToolchain_overrideSettingsKeyMissingWarns(t *testing.T) {
	bundlePath := seedToolchainBundle(t, bundleOpts{dropKey: "SWIFT_ENABLE_COMPILE_CACHE"})

	res := diagnoseXcodeToolchain(bundlePath)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "out of date")
}

func TestDiagnoseXcodeToolchain_shimMissingWarns(t *testing.T) {
	bundlePath := seedToolchainBundle(t, bundleOpts{dropShim: "swiftc"})

	res := diagnoseXcodeToolchain(bundlePath)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "swiftc")
	assert.Contains(t, res.Detail, "missing/non-executable")
}

func TestDiagnoseXcodeToolchain_shimNonExecutableWarns(t *testing.T) {
	bundlePath := seedToolchainBundle(t, bundleOpts{nonExecShim: "clang"})

	res := diagnoseXcodeToolchain(bundlePath)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "clang")
	assert.Contains(t, res.Detail, "missing/non-executable")
}

func TestDiagnoseXcodeToolchain_okAllGatesPass(t *testing.T) {
	bundlePath := seedToolchainBundle(t, bundleOpts{skipPlist: true})

	// Exercise the real producer end-to-end: render → write → doctor-parse.
	body, err := xa.RenderToolchainInfoPlist("/tmp/sentinel-proxy.sock", "/tmp/sentinel-plugin.dylib")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(bundlePath, xa.ToolchainInfoPlistFile), body, 0o644)) //nolint:gosec

	res := diagnoseXcodeToolchain(bundlePath)

	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, bundlePath)
	assert.Contains(t, res.Detail, "clang")
	assert.Contains(t, res.Detail, "swift")
	assert.Contains(t, res.Detail, "swiftc")
}

func TestDiagnoseXcodeToolchain_symlinkShimOK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests are POSIX-only")
	}

	bundlePath := seedToolchainBundle(t, bundleOpts{symlinkShims: true})

	res := diagnoseXcodeToolchain(bundlePath)

	assert.Equal(t, StateOK, res.State)
}

func TestDiagnoseXcodeToolchain_danglingShimSymlinkWarns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests are POSIX-only")
	}

	bundlePath := seedToolchainBundle(t, bundleOpts{danglingShim: "swift"})

	res := diagnoseXcodeToolchain(bundlePath)

	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "swift")
	assert.Contains(t, res.Detail, "missing/non-executable")
}

type bundleOpts struct {
	skipPlist       bool
	wrongIdentifier bool
	dropKey         string
	dropShim        string
	nonExecShim     string
	symlinkShims    bool
	danglingShim    string
}

func seedToolchainBundle(t *testing.T, opts bundleOpts) string {
	t.Helper()

	bundlePath := filepath.Join(t.TempDir(), "bundle")
	require.NoError(t, os.MkdirAll(bundlePath, 0o755))

	if !opts.skipPlist {
		body := buildPlist(opts)
		require.NoError(t, os.WriteFile(filepath.Join(bundlePath, xa.ToolchainInfoPlistFile), body, 0o644)) //nolint:gosec
	}

	binDir := filepath.Join(bundlePath, "usr", "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	var symlinkTarget string
	if opts.symlinkShims {
		target := filepath.Join(t.TempDir(), "fake-apple-tc", "usr", "bin")
		require.NoError(t, os.MkdirAll(target, 0o755))
		for _, name := range toolchainUsrBinShims {
			require.NoError(t, os.WriteFile(filepath.Join(target, name), []byte("#!/bin/sh\n"), 0o755)) //nolint:gosec
		}
		symlinkTarget = target
	}

	for _, name := range toolchainUsrBinShims {
		if name == opts.dropShim {
			continue
		}
		dst := filepath.Join(binDir, name)
		switch {
		case name == opts.danglingShim:
			require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "does-not-exist", name), dst))
		case name == opts.nonExecShim:
			require.NoError(t, os.WriteFile(dst, []byte("#!/bin/sh\n"), 0o644)) //nolint:gosec
		case symlinkTarget != "":
			require.NoError(t, os.Symlink(filepath.Join(symlinkTarget, name), dst))
		default:
			require.NoError(t, os.WriteFile(dst, []byte("#!/bin/sh\n"), 0o755)) //nolint:gosec
		}
	}

	return bundlePath
}

func buildPlist(opts bundleOpts) []byte {
	identifier := xa.ToolchainID
	if opts.wrongIdentifier {
		identifier = "io.bogus.toolchain"
	}

	var settings string
	for _, k := range xa.ToolchainOverrideBuildSettingsKeys {
		if k == opts.dropKey {
			continue
		}
		settings += "\t\t<key>" + k + "</key>\n\t\t<string>YES</string>\n"
	}

	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Identifier</key>
	<string>` + identifier + `</string>
	<key>OverrideBuildSettings</key>
	<dict>
` + settings + `	</dict>
</dict>
</plist>
`)
}
