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
//
// toolchainInstalled gates the `TOOLCHAINS = <id>` line: install is
// best-effort, and emitting the setting when the bundle is missing (or was
// removed by the user) would make Xcode fail to resolve the toolchain for SPM
// package targets.
func Render(proxySocketPath, pluginPath string, toolchainInstalled bool) (string, error) {
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
	}
	if toolchainInstalled {
		// Bundle's OverrideBuildSettings is the only reach we have into SPM package targets.
		settings["TOOLCHAINS"] = ToolchainID
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
	if !toolchainInstalled {
		b.WriteString("// TOOLCHAINS = " + ToolchainID + " omitted: toolchain bundle not installed; SPM targets will miss the CAS wiring.\n\n")
	}

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

// WriteOverrideXCConfig renders and persists the IDE override xcconfig. The
// toolchain probe runs here (not inside Render) because the writer knows the
// user's home dir and so can resolve the install/link paths; Render stays a
// pure string builder.
//
// casPluginPath is injected by the caller so the IDE xcconfig and the toolchain
// bundle's OverrideBuildSettings resolve the plugin path through the same
// resolver — see internal/config/xcelerate.ResolveCASPluginPath.
func WriteOverrideXCConfig(osProxy utils.OsProxy, envs map[string]string, proxySocketPath, casPluginPath string) error {
	home, err := osProxy.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	p := paths.FromHome(home)

	body, err := Render(proxySocketPath, casPluginPath, ToolchainInstalled(p.XcodeToolchainBundleDir(), p.XcodeToolchainsLinkPath()))
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
