// Package xcode_app writes the Xcode.app IDE override xcconfig and wires it
// into an .xcodeproj via baseConfigurationReference. macOS-only.
package xcode_app

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// AppleCASPluginPath is the stock LLVM CAS plugin dylib that ships with Xcode.
// Apple's plugin speaks the same gRPC protocol as our proxy — see
// docs/xcode-app-ide-remote-cas-findings-2026-09-30.md.
const AppleCASPluginPath = "/Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib"

// Render returns the override xcconfig body that engages remote CAS through
// Bitrise's xcelerate-proxy.
//
// The template deliberately omits COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES:
// SwiftBuild's CompilationCachingConfigFileTaskProducer bails (`return nil`)
// when both REMOTE_SERVICE_PATH and SUPPORTED_LANGUAGES are set, so no
// `.cas-config` is written and remote never engages.
func Render(proxySocketPath string) (string, error) {
	if strings.TrimSpace(proxySocketPath) == "" {
		return "", errors.New("proxy socket path is empty")
	}

	settings := map[string]string{
		"CLANG_ENABLE_COMPILE_CACHE":            "YES",
		"CLANG_ENABLE_MODULES":                  "YES",
		"COMPILATION_CACHE_ENABLE_CACHING":      "YES",
		"COMPILATION_CACHE_ENABLE_PLUGIN":       "YES",
		"COMPILATION_CACHE_PLUGIN_PATH":         AppleCASPluginPath,
		"COMPILATION_CACHE_REMOTE_SERVICE_PATH": proxySocketPath,
		"OTHER_SWIFT_FLAGS":                     "$(inherited) -cas-plugin-option remote-service-path=" + proxySocketPath,
		"SWIFT_ENABLE_COMPILE_CACHE":            "YES",
	}

	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("// Bitrise Build Cache — Xcode.app IDE override\n")
	b.WriteString("// Written by `bitrise-build-cache activate xcode`. Removed by `deactivate xcode`.\n")
	b.WriteString("// Do not edit by hand.\n\n")

	for _, k := range keys {
		fmt.Fprintf(&b, "%s = %s\n", k, settings[k])
	}

	return b.String(), nil
}

// EnvOverrideXCConfigPath overrides the on-disk location of the Xcode.app
// override xcconfig. Callers rarely need it; kept so the writer (`activate
// xcode`) and the readers (`xcode-app link`, doctor) resolve to one path.
const EnvOverrideXCConfigPath = "BITRISE_XCODE_APP_OVERRIDE_XCCONFIG_PATH"

// ResolveOverrideXCConfigPath returns the override xcconfig path in the same
// order the writer and readers use: explicit override → env var → default
// ~/.bitrise-xcelerate/xcode-app.xcconfig. Returns ("", err) only when the
// default path is in play and the home dir cannot be resolved.
func ResolveOverrideXCConfigPath(override string, envs map[string]string, osProxy utils.OsProxy) (string, error) {
	if override != "" {
		return override, nil
	}
	if env := envs[EnvOverrideXCConfigPath]; env != "" {
		return env, nil
	}

	home, err := osProxy.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}

	return paths.FromHome(home).XcodeAppOverrideXCConfigFile(), nil
}

// WriteOverrideXCConfig renders the override xcconfig and writes it to the
// path resolved via ResolveOverrideXCConfigPath, creating the dir if needed.
// Called as a side effect of `activate xcode` so `xcode-app link` has something
// to point at.
func WriteOverrideXCConfig(osProxy utils.OsProxy, envs map[string]string, proxySocketPath string) error {
	body, err := Render(proxySocketPath)
	if err != nil {
		return fmt.Errorf("render override xcconfig: %w", err)
	}

	path, err := ResolveOverrideXCConfigPath("", envs, osProxy)
	if err != nil {
		return err
	}

	if err := osProxy.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	if err := osProxy.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // xcconfig must be readable by Xcode
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
