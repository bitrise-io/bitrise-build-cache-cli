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

// AppleCASPluginPath is the stock LLVM CAS plugin dylib that ships with Xcode;
// it speaks the same gRPC protocol as our proxy.
const AppleCASPluginPath = "/Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib"

// Render omits COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES on purpose:
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
		// TOOLCHAINS selects the Bitrise toolchain bundle installed under
		// ~/Library/Developer/Toolchains/. The bundle's OverrideBuildSettings is
		// the only reach we have into SPM package targets.
		"TOOLCHAINS": ToolchainID,
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

const EnvOverrideXCConfigPath = "BITRISE_XCODE_APP_OVERRIDE_XCCONFIG_PATH"

// ResolveOverrideXCConfigPath resolves in order: explicit override, env var,
// then default under ~/.bitrise-xcelerate. Fails only when the default is in
// play and the home dir cannot be resolved.
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
