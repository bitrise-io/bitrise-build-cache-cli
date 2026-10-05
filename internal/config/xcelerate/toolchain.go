package xcelerate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

// installXcodeToolchain installs the thin toolchain bundle under
// ~/.bitrise-xcelerate/toolchain/<id> and links it into
// ~/Library/Developer/Toolchains so Xcode discovers it.
//
// The bundle's OverrideBuildSettings is the only mechanism that reaches SPM
// package targets on CLI xcodebuild runs. Failures are non-fatal: a missing
// toolchain just means SPM targets fall back to no cache, same as before.
func installXcodeToolchain(
	ctx context.Context,
	logger log.Logger,
	osProxy utils.OsProxy,
	cmdFunc utils.CommandFunc,
	proxySocketPath, originalXcodebuildPath string,
) {
	home, err := osProxy.UserHomeDir()
	if err != nil {
		logger.Warnf("Could not resolve home dir for Xcode toolchain install: %s", err)

		return
	}

	developerDir, err := resolveDeveloperDir(ctx, cmdFunc, originalXcodebuildPath)
	if err != nil {
		logger.Warnf("Could not resolve Xcode developer dir for toolchain install: %s", err)

		return
	}

	p := paths.FromHome(home)
	installPath := p.XcodeToolchainBundleDir()
	linkPath := p.XcodeToolchainsLinkPath()
	defaultToolchainPath := filepath.Join(developerDir, "Toolchains", "XcodeDefault.xctoolchain")
	pluginPath := filepath.Join(developerDir, "usr", "lib", "libToolchainCASPlugin.dylib")

	if err := xcode_app.InstallToolchain(installPath, defaultToolchainPath, proxySocketPath, pluginPath); err != nil {
		logger.Warnf("Could not install Xcode toolchain bundle: %s", err)

		return
	}

	if err := xcode_app.LinkToolchain(installPath, linkPath); err != nil {
		logger.Warnf("Could not link Xcode toolchain bundle at %s: %s", linkPath, err)

		return
	}

	logger.Infof("Installed Xcode toolchain bundle at %s (linked under %s)", installPath, linkPath)
}

// uninstallXcodeToolchain removes the installed bundle plus discovery symlink,
// mirroring installXcodeToolchain's two steps in reverse. Failures are
// non-fatal — deactivate must not block on a stale toolchain.
func uninstallXcodeToolchain(logger log.Logger, osProxy utils.OsProxy) {
	home, err := osProxy.UserHomeDir()
	if err != nil {
		logger.Debugf("Could not resolve home dir for Xcode toolchain uninstall: %s", err)

		return
	}

	p := paths.FromHome(home)
	installPath := p.XcodeToolchainBundleDir()
	linkPath := p.XcodeToolchainsLinkPath()

	if err := xcode_app.UninstallToolchain(installPath, linkPath); err != nil {
		logger.Warnf("Could not uninstall Xcode toolchain bundle: %s", err)

		return
	}

	logger.Infof("Removed Xcode toolchain bundle at %s", installPath)
}

// resolveDeveloperDir prefers deriving from the known xcodebuild path so the
// toolchain install matches the CLI's configured Xcode, and falls back to
// xcode-select -p for the stock terminal / Build Hub flows.
func resolveDeveloperDir(ctx context.Context, cmdFunc utils.CommandFunc, originalXcodebuildPath string) (string, error) {
	if derived := developerDirFromXcodebuildPath(originalXcodebuildPath); derived != "" {
		return derived, nil
	}

	cmd := cmdFunc(ctx, "xcode-select", "-p")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("xcode-select -p: %w", err)
	}
	developerDir := strings.TrimSpace(string(out))
	if developerDir == "" {
		return "", errors.New("xcode-select -p returned an empty developer dir")
	}

	return developerDir, nil
}

// developerDirFromXcodebuildPath returns the active Xcode's developer dir if
// `path` looks like `<DeveloperDir>/usr/bin/xcodebuild`; otherwise returns ""
// so the caller falls back to `xcode-select -p`.
func developerDirFromXcodebuildPath(path string) string {
	const suffix = "/usr/bin/xcodebuild"
	if !strings.HasSuffix(path, suffix) {
		return ""
	}

	return strings.TrimSuffix(path, suffix)
}
