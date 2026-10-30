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

// EnvCASPluginPath mirrors the toolchain-side override: when set, both the
// toolchain install path resolver and this xcconfig emit the same dylib so the
// IDE and SPM flows stay in lockstep.
const EnvCASPluginPath = "BITRISE_XCODE_CAS_PLUGIN_PATH"

// Render omits COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES on purpose:
// SwiftBuild's CompilationCachingConfigFileTaskProducer bails (`return nil`)
// when both REMOTE_SERVICE_PATH and SUPPORTED_LANGUAGES are set, so no
// `.cas-config` is written and remote never engages.
func Render(proxySocketPath, pluginPath string) (string, error) {
	if strings.TrimSpace(proxySocketPath) == "" {
		return "", errors.New("proxy socket path is empty")
	}
	if strings.TrimSpace(pluginPath) == "" {
		return "", errors.New("CAS plugin path is empty")
	}

	settings := map[string]string{
		"CLANG_ENABLE_COMPILE_CACHE":                  "YES",
		"CLANG_ENABLE_MODULES":                        "YES",
		"COMPILATION_CACHE_ENABLE_CACHING":            "YES",
		"COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS": "YES",
		"COMPILATION_CACHE_ENABLE_PLUGIN":             "YES",
		"COMPILATION_CACHE_PLUGIN_PATH":               pluginPath,
		"COMPILATION_CACHE_REMOTE_SERVICE_PATH":       proxySocketPath,
		"SWIFT_ENABLE_COMPILE_CACHE":                  "YES",
		// Bundle's OverrideBuildSettings is the only reach we have into SPM package targets.
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
// default under ~/.bitrise-xcelerate.
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

// ResolveCASPluginPath returns the plugin dylib path emitted into the override
// xcconfig: BITRISE_XCODE_CAS_PLUGIN_PATH when set, anchor-rooted otherwise
// (`/var/db/xcode_select_link/...`, so `xcode-select -s` auto-corrects).
func ResolveCASPluginPath(envs map[string]string) string {
	if env := envs[EnvCASPluginPath]; env != "" {
		return env
	}

	return paths.XcodeSelectLinkCASPluginPath()
}

func WriteOverrideXCConfig(osProxy utils.OsProxy, envs map[string]string, proxySocketPath string) error {
	body, err := Render(proxySocketPath, ResolveCASPluginPath(envs))
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
