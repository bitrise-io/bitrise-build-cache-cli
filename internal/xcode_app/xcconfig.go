package xcode_app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
)

const RemoteServicePathKey = "COMPILATION_CACHE_REMOTE_SERVICE_PATH"

func RenderOverride(proxySocketPath, previousIncludePath, homeDir string) (string, error) {
	if strings.TrimSpace(proxySocketPath) == "" {
		return "", fmt.Errorf("proxy socket path is empty")
	}

	if strings.TrimSpace(homeDir) == "" {
		return "", fmt.Errorf("home directory is empty — required for -fdepscan-prefix-map to make CAS keys machine-independent")
	}

	// xcconfig `#include "<path>"` has no documented quote-escape — reject quotes rather than emit a silently malformed file.
	if strings.ContainsRune(previousIncludePath, '"') {
		return "", fmt.Errorf("previous XCODE_XCCONFIG_FILE path contains a quote character — cannot safely #include")
	}

	extras := map[string]string{
		RemoteServicePathKey:                          proxySocketPath,
		"CLANG_ENABLE_PREFIX_MAPPING":                 "YES",
		"SWIFT_ENABLE_PREFIX_MAPPING":                 "YES",
		"COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS": "NO",
		"OTHER_CFLAGS":                                fmt.Sprintf("$(inherited) -fdepscan-prefix-map=%s=/^home", homeDir),
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

	keys := make([]string, 0, len(xcodeargs.CacheArgs)+len(extras))
	for k := range xcodeargs.CacheArgs {
		keys = append(keys, k)
	}
	for k := range extras {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if v, ok := extras[k]; ok {
			fmt.Fprintf(&b, "%s = %s\n", k, v)

			continue
		}

		fmt.Fprintf(&b, "%s = %s\n", k, xcodeargs.CacheArgs[k])
	}

	return b.String(), nil
}
