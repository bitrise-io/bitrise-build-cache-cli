// Package xcode_app drives Xcode.app (GUI) build-cache enable/disable via
// launchctl + an override xcconfig. macOS-only.
package xcode_app

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// XCConfigEnvVar is the well-known Xcode env var that names a workspace-scope
// xcconfig to layer on top of every scheme's build settings.
const XCConfigEnvVar = "XCODE_XCCONFIG_FILE"

// AppleCASPluginPath is the stock LLVM CAS plugin dylib that ships with Xcode.
// Apple's plugin speaks the same gRPC protocol as our proxy — see
// docs/xcode-app-ide-remote-cas-findings-2026-09-30.md.
const AppleCASPluginPath = "/Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib"

// Render returns the override xcconfig body that engages remote CAS through
// Bitrise's xcelerate-proxy. If previousIncludePath is non-empty, it is chained
// in via `#include?` so we do not clobber the user's own override.
//
// The template deliberately omits COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES:
// SwiftBuild's CompilationCachingConfigFileTaskProducer bails (`return nil`)
// when both REMOTE_SERVICE_PATH and SUPPORTED_LANGUAGES are set, so no
// `.cas-config` is written and remote never engages.
func Render(proxySocketPath, previousIncludePath string) (string, error) {
	if strings.TrimSpace(proxySocketPath) == "" {
		return "", errors.New("proxy socket path is empty")
	}

	// xcconfig `#include?` has no documented quote-escape — reject rather than
	// silently emit a malformed file.
	if strings.ContainsRune(previousIncludePath, '"') {
		return "", errors.New("previous XCODE_XCCONFIG_FILE path contains a quote character — cannot safely #include")
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
	b.WriteString("// Written by `bitrise-build-cache xcode-app enable`. Removed by `xcode-app disable`.\n")
	b.WriteString("// Do not edit by hand.\n\n")

	if previousIncludePath != "" {
		fmt.Fprintf(&b, "#include? \"%s\"\n\n", previousIncludePath)
	}

	for _, k := range keys {
		fmt.Fprintf(&b, "%s = %s\n", k, settings[k])
	}

	return b.String(), nil
}
