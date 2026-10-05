package xcode_app

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// Toolchain bundle identity. ToolchainID is the single source of truth, also
// consumed by the xcconfig writer so the TOOLCHAINS build setting resolves the
// bundle Xcode discovers.
const (
	ToolchainID                          = paths.XcodeToolchainBundleID
	ToolchainDisplayName                 = "Bitrise Build Cache"
	ToolchainCompatibilityVersion        = 2
	ToolchainCompatibilityVersionDisplay = "Xcode 15.0 or later"
	toolchainInfoPlistFile               = "ToolchainInfo.plist"
)

// toolchainSymlinkedUsrDirs mirror the Default toolchain's layout under the
// bundle's usr/, so Xcode's `TOOLCHAINS` lookup resolves every binary / lib /
// header exactly as the Default toolchain would.
var toolchainSymlinkedUsrDirs = []string{"lib", "libexec", "include", "share"} //nolint:gochecknoglobals // immutable layout data

// RenderToolchainInfoPlist produces the ToolchainInfo.plist body carrying the
// compile-cache build-setting overrides. `pluginPath` is Apple's
// libToolchainCASPlugin.dylib (derived from the active Xcode developer dir —
// never hardcoded to /Applications/Xcode.app so Xcode-beta / sidecar
// installs work).
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

// InstallToolchain writes a thin toolchain bundle under installPath that
// shadows every binary of defaultToolchainPath via symlink and carries
// OverrideBuildSettings for the CAS wiring. Idempotent: re-running produces
// the same bundle state.
func InstallToolchain(installPath, defaultToolchainPath, proxySocketPath, pluginPath string) error {
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

	if err := symlinkUsrBinEntries(installPath, defaultToolchainPath); err != nil {
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

// UninstallToolchain removes the installed bundle and the discovery symlink
// under ~/Library/Developer/Toolchains if it points at installPath.
// `toolchainsLinkPath` is the full path of the discovery symlink
// (`~/Library/Developer/Toolchains/<id>`).
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

// LinkToolchain places a symlink at toolchainsLinkPath pointing at installPath
// so Xcode discovers the bundle via its canonical lookup dir.
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

	// Idempotency: replace any existing entry at the link path so a stale
	// pointer from an earlier install doesn't shadow the fresh bundle.
	if _, err := os.Lstat(toolchainsLinkPath); err == nil {
		if err := os.RemoveAll(toolchainsLinkPath); err != nil {
			return fmt.Errorf("replace existing toolchains link %s: %w", toolchainsLinkPath, err)
		}
	}

	if err := os.Symlink(installPath, toolchainsLinkPath); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", toolchainsLinkPath, installPath, err)
	}

	return nil
}

// Private — toolchain bundle layout helpers.

func symlinkUsrBinEntries(installPath, defaultToolchainPath string) error {
	srcBin := filepath.Join(defaultToolchainPath, "usr", "bin")
	dstBin := filepath.Join(installPath, "usr", "bin")

	if err := os.MkdirAll(dstBin, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dstBin, err)
	}

	entries, err := os.ReadDir(srcBin)
	if err != nil {
		return fmt.Errorf("read default toolchain usr/bin %s: %w", srcBin, err)
	}

	for _, e := range entries {
		src := filepath.Join(srcBin, e.Name())
		dst := filepath.Join(dstBin, e.Name())
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
		}
	}

	return nil
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

// marshalToolchainPlist renders a minimal Apple-style XML plist. Hand-written
// because encoding/xml does not emit the DOCTYPE and dict/key/string pairs
// Xcode's plist parser expects.
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
