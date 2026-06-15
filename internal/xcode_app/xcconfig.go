package xcode_app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/xcelerate/xcodeargs"
)

// RemoteServicePathKey is the xcconfig key Xcode reads to discover the
// xcelerate-proxy unix socket. Mirrors the additional[] entry that
// cmd/xcode/xcodebuild.go injects via `xcodebuild` CLI args.
const RemoteServicePathKey = "COMPILATION_CACHE_REMOTE_SERVICE_PATH"

// RenderOverride returns the xcconfig content we write to
// ~/.bitrise-xcelerate/xcode-app.xcconfig.
//
// previousIncludePath is the prior `XCODE_XCCONFIG_FILE` value captured at
// enable time. When non-empty, the rendered file leads with
//
//	#include "<previousIncludePath>"
//
// so the user's existing xcconfig keys take effect except where our cache
// keys override (the override slot has higher precedence than per-target/base
// xcconfigs but lower than `xcodebuild` CLI args — see the E1 spike doc).
//
// proxySocketPath is the absolute path of the xcelerate-proxy unix socket.
// Must be non-empty; an empty path is a programmer error (caller should have
// validated the xcelerate config exists).
func RenderOverride(proxySocketPath, previousIncludePath string) (string, error) {
	if strings.TrimSpace(proxySocketPath) == "" {
		return "", fmt.Errorf("proxy socket path is empty")
	}

	var b strings.Builder

	b.WriteString("// Bitrise Build Cache — Xcode.app override\n")
	b.WriteString("// Written by `bitrise-build-cache xcode-app enable`.\n")
	b.WriteString("// Removed by `bitrise-build-cache xcode-app disable`.\n")
	b.WriteString("// Do not edit by hand.\n\n")

	if previousIncludePath != "" {
		// Chain the prior override so we don't clobber the user's setup
		// (E3 — ACI-5042). Quoted form supports paths with spaces.
		fmt.Fprintf(&b, "#include \"%s\"\n\n", previousIncludePath)
	}

	keys := make([]string, 0, len(xcodeargs.CacheArgs)+1)
	keys = append(keys, RemoteServicePathKey)
	for k := range xcodeargs.CacheArgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		switch k {
		case RemoteServicePathKey:
			fmt.Fprintf(&b, "%s = %s\n", k, proxySocketPath)
		default:
			fmt.Fprintf(&b, "%s = %s\n", k, xcodeargs.CacheArgs[k])
		}
	}

	return b.String(), nil
}
