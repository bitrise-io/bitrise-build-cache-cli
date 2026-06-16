package xcode_app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/xcelerate/xcodeargs"
)

const RemoteServicePathKey = "COMPILATION_CACHE_REMOTE_SERVICE_PATH"

func RenderOverride(proxySocketPath, previousIncludePath string) (string, error) {
	if strings.TrimSpace(proxySocketPath) == "" {
		return "", fmt.Errorf("proxy socket path is empty")
	}

	// xcconfig `#include "<path>"` has no documented quote-escape — reject quotes rather than emit a silently malformed file.
	if strings.ContainsRune(previousIncludePath, '"') {
		return "", fmt.Errorf("previous XCODE_XCCONFIG_FILE path contains a quote character — cannot safely #include")
	}

	var b strings.Builder

	b.WriteString("// Bitrise Build Cache — Xcode.app override\n")
	b.WriteString("// Written by `bitrise-build-cache xcode-app enable`.\n")
	b.WriteString("// Removed by `bitrise-build-cache xcode-app disable`.\n")
	b.WriteString("// Do not edit by hand.\n\n")

	if previousIncludePath != "" {
		// Chain the prior override so we don't clobber the user's setup. Quoted form supports paths with spaces.
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
