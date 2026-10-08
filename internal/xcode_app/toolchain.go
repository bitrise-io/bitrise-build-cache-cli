package xcode_app

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// ToolchainID is shared with the TOOLCHAINS xcconfig setting so SwiftBuild
// resolves the same bundle.
const (
	ToolchainID                          = paths.XcodeToolchainBundleID
	ToolchainDisplayName                 = "Bitrise Build Cache"
	ToolchainCompatibilityVersion        = 2
	ToolchainCompatibilityVersionDisplay = "Xcode 15.0 or later"
	toolchainInfoPlistFile               = "ToolchainInfo.plist"
)

var toolchainSymlinkedUsrDirs = []string{"lib", "libexec", "include", "share"} //nolint:gochecknoglobals // immutable layout data

// trampolinedBins is the set of usr/bin entries that get a trampoline copy
// instead of a symlink. SwiftBuild only invokes these three with the toolchain
// prefix; every other entry stays as a real-toolchain symlink so SwiftBuild's
// clang-stat-cache / ld / llvm-cas / bitcode_strip lookups keep resolving.
var trampolinedBins = map[string]struct{}{ //nolint:gochecknoglobals // immutable layout data
	"swiftc": {},
	"clang":  {},
	"swift":  {},
}

// RenderToolchainInfoPlist builds a ToolchainInfo.plist carrying the
// compile-cache OverrideBuildSettings. pluginPath is derived from the active
// developer dir so Xcode-beta / sidecar installs work.
func RenderToolchainInfoPlist(proxySocketPath, pluginPath string) ([]byte, error) {
	if proxySocketPath == "" {
		return nil, errors.New("proxy socket path is empty")
	}
	if pluginPath == "" {
		return nil, errors.New("CAS plugin path is empty")
	}

	settings := map[string]string{
		"CLANG_ENABLE_COMPILE_CACHE":            "YES",
		"CLANG_ENABLE_MODULES":                  "YES",
		"COMPILATION_CACHE_ENABLE_CACHING":      "YES",
		"COMPILATION_CACHE_ENABLE_PLUGIN":       "YES",
		"COMPILATION_CACHE_PLUGIN_PATH":         pluginPath,
		"COMPILATION_CACHE_REMOTE_SERVICE_PATH": proxySocketPath,
		"OTHER_SWIFT_FLAGS":                     "$(inherited) -cas-plugin-option remote-service-path=" + proxySocketPath,
		"SWIFT_ENABLE_COMPILE_CACHE":            "YES",
	}

	return []byte(marshalToolchainPlist(settings)), nil
}

// InstallToolchain writes a thin toolchain bundle that shadows
// defaultToolchainPath via symlinks and carries CAS OverrideBuildSettings.
// Idempotent. When trampolinePath is non-empty, {swiftc, clang, swift} are
// copies of that binary (ad-hoc code-signed) instead of symlinks; every other
// usr/bin entry stays as a symlink to the default toolchain.
func InstallToolchain(installPath, defaultToolchainPath, proxySocketPath, pluginPath, trampolinePath string) error {
	if installPath == "" {
		return errors.New("install path is empty")
	}
	if defaultToolchainPath == "" {
		return errors.New("default toolchain path is empty")
	}

	if err := os.RemoveAll(installPath); err != nil {
		return fmt.Errorf("clean existing toolchain bundle %s: %w", installPath, err)
	}
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		return fmt.Errorf("create toolchain bundle dir %s: %w", installPath, err)
	}

	body, err := RenderToolchainInfoPlist(proxySocketPath, pluginPath)
	if err != nil {
		return fmt.Errorf("render ToolchainInfo.plist: %w", err)
	}
	plistPath := filepath.Join(installPath, toolchainInfoPlistFile)
	if err := os.WriteFile(plistPath, body, 0o644); err != nil { //nolint:gosec // plist must be readable by Xcode
		return fmt.Errorf("write %s: %w", plistPath, err)
	}

	if err := installUsrBinEntries(installPath, defaultToolchainPath, trampolinePath); err != nil {
		return err
	}
	if err := symlinkTopLevelUsrDirs(installPath, defaultToolchainPath); err != nil {
		return err
	}
	if err := symlinkDeveloperDir(installPath, defaultToolchainPath); err != nil {
		return err
	}

	return nil
}

// UninstallToolchain removes the bundle and the discovery symlink, but only
// drops the symlink when it still points at installPath (so we don't clobber
// a foreign bundle that reused the same name).
func UninstallToolchain(installPath, toolchainsLinkPath string) error {
	if toolchainsLinkPath != "" {
		if target, err := os.Readlink(toolchainsLinkPath); err == nil && target == installPath {
			if err := os.Remove(toolchainsLinkPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("remove toolchain link %s: %w", toolchainsLinkPath, err)
			}
		}
	}

	if installPath == "" {
		return nil
	}

	if err := os.RemoveAll(installPath); err != nil {
		return fmt.Errorf("remove toolchain bundle %s: %w", installPath, err)
	}

	return nil
}

// LinkToolchain creates the discovery symlink. Replaces an existing symlink
// (stale pointer); refuses to clobber a non-symlink the user put there.
func LinkToolchain(installPath, toolchainsLinkPath string) error {
	if toolchainsLinkPath == "" {
		return errors.New("toolchains link path is empty")
	}
	if installPath == "" {
		return errors.New("install path is empty")
	}

	if err := os.MkdirAll(filepath.Dir(toolchainsLinkPath), 0o755); err != nil {
		return fmt.Errorf("create toolchains link dir %s: %w", filepath.Dir(toolchainsLinkPath), err)
	}

	if info, err := os.Lstat(toolchainsLinkPath); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("toolchains link path %s exists and is not a symlink; refusing to replace", toolchainsLinkPath)
		}
		if err := os.Remove(toolchainsLinkPath); err != nil {
			return fmt.Errorf("replace existing toolchains link %s: %w", toolchainsLinkPath, err)
		}
	}

	if err := os.Symlink(installPath, toolchainsLinkPath); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", toolchainsLinkPath, installPath, err)
	}

	return nil
}

// installUsrBinEntries copies trampolinePath over {swiftc, clang, swift} when
// trampolinePath is non-empty and resolves; symlinks everything else to the
// default toolchain. Empty trampolinePath = pure-symlink layout (graceful
// fallback when the trampoline download failed).
func installUsrBinEntries(installPath, defaultToolchainPath, trampolinePath string) error {
	srcBin := filepath.Join(defaultToolchainPath, "usr", "bin")
	dstBin := filepath.Join(installPath, "usr", "bin")

	if err := os.MkdirAll(dstBin, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dstBin, err)
	}

	entries, err := os.ReadDir(srcBin)
	if err != nil {
		return fmt.Errorf("read default toolchain usr/bin %s: %w", srcBin, err)
	}

	trampolineReady := trampolinePath != "" && isExecutable(trampolinePath)

	for _, e := range entries {
		src := filepath.Join(srcBin, e.Name())
		dst := filepath.Join(dstBin, e.Name())

		if _, ok := trampolinedBins[e.Name()]; ok && trampolineReady {
			if err := installTrampolineCopy(trampolinePath, dst); err != nil {
				return err
			}

			continue
		}

		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
		}
	}

	return nil
}

func installTrampolineCopy(trampolinePath, dst string) error {
	src, err := os.Open(trampolinePath) //nolint:gosec // trampolinePath comes from paths.TrampolineBinary
	if err != nil {
		return fmt.Errorf("open trampoline %s: %w", trampolinePath, err)
	}
	defer func() { _ = src.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("open trampoline dst %s: %w", dst, err)
	}

	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()

		return fmt.Errorf("copy trampoline to %s: %w", dst, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close trampoline dst %s: %w", dst, err)
	}

	codesignTrampoline(dst)

	return nil
}

// codesignTrampoline runs an ad-hoc code-sign on the installed shim. macOS
// refuses to launch an unsigned binary from the toolchain bundle, so this is
// not optional. Failures are swallowed because codesign is absent on non-mac
// test runs where the install still needs to succeed structurally.
func codesignTrampoline(path string) {
	//nolint:gosec // fixed binary + path from our own install
	cmd := exec.Command("/usr/bin/codesign", "--sign", "-", "--force", "--timestamp=none", path)
	_ = cmd.Run()
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}

	return fi.Mode().IsRegular() && (fi.Mode().Perm()&0o111) != 0
}

func symlinkTopLevelUsrDirs(installPath, defaultToolchainPath string) error {
	for _, name := range toolchainSymlinkedUsrDirs {
		src := filepath.Join(defaultToolchainPath, "usr", name)
		if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
			continue
		}

		dst := filepath.Join(installPath, "usr", name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(dst), err)
		}
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
		}
	}

	return nil
}

func symlinkDeveloperDir(installPath, defaultToolchainPath string) error {
	src := filepath.Join(defaultToolchainPath, "Developer")
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	dst := filepath.Join(installPath, "Developer")
	if err := os.Symlink(src, dst); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
	}

	return nil
}

// marshalToolchainPlist is hand-written because encoding/xml does not emit the
// DOCTYPE Xcode's plist parser requires.
func marshalToolchainPlist(overrideSettings map[string]string) string {
	keys := make([]string, 0, len(overrideSettings))
	for k := range overrideSettings {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`)
	b.WriteString("\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString("<dict>\n")
	writeKey(&b, "Identifier")
	writeString(&b, ToolchainID)
	writeKey(&b, "DisplayName")
	writeString(&b, ToolchainDisplayName)
	writeKey(&b, "CompatibilityVersion")
	fmt.Fprintf(&b, "\t<integer>%d</integer>\n", ToolchainCompatibilityVersion)
	writeKey(&b, "CompatibilityVersionDisplayString")
	writeString(&b, ToolchainCompatibilityVersionDisplay)
	writeKey(&b, "OverrideBuildSettings")
	b.WriteString("\t<dict>\n")
	for _, k := range keys {
		b.WriteString("\t\t<key>")
		b.WriteString(xmlEscape(k))
		b.WriteString("</key>\n\t\t<string>")
		b.WriteString(xmlEscape(overrideSettings[k]))
		b.WriteString("</string>\n")
	}
	b.WriteString("\t</dict>\n")
	b.WriteString("</dict>\n</plist>\n")

	return b.String()
}

func writeKey(b *strings.Builder, key string) {
	b.WriteString("\t<key>")
	b.WriteString(xmlEscape(key))
	b.WriteString("</key>\n")
}

func writeString(b *strings.Builder, s string) {
	b.WriteString("\t<string>")
	b.WriteString(xmlEscape(s))
	b.WriteString("</string>\n")
}

func xmlEscape(s string) string {
	var buf strings.Builder
	_ = xml.EscapeText(&buf, []byte(s))

	return buf.String()
}
