package xcelerate

import (
	"context"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/devdir"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

// installXcodeToolchain installs the toolchain bundle and links it under
// ~/Library/Developer/Toolchains. Best-effort: failures just mean SPM targets
// miss the CAS wiring.
func installXcodeToolchain(
	ctx context.Context,
	logger log.Logger,
	osProxy utils.OsProxy,
	cmdFunc utils.CommandFunc,
	envs map[string]string,
	proxySocketPath, originalXcodebuildPath string,
) {
	home, err := osProxy.UserHomeDir()
	if err != nil {
		logger.Warnf("Could not resolve home dir for Xcode toolchain install: %s", err)

		return
	}

	developerDir, err := devdir.Resolve(ctx, cmdFunc, originalXcodebuildPath)
	if err != nil {
		logger.Warnf("Could not resolve Xcode developer dir for toolchain install: %s", err)

		return
	}

	p := paths.FromHome(home)
	installPath := p.XcodeToolchainBundleDir()
	linkPath := p.XcodeToolchainsLinkPath()
	defaultToolchainPath := filepath.Join(developerDir, "Toolchains", "XcodeDefault.xctoolchain")
	pluginPath := ResolveCASPluginPath("", envs, developerDir, osProxy)

	if err := xcode_app.InstallToolchain(ctx, installPath, defaultToolchainPath, proxySocketPath, pluginPath); err != nil {
		logger.Warnf("Could not install Xcode toolchain bundle: %s", err)

		return
	}

	if err := xcode_app.LinkToolchain(installPath, linkPath); err != nil {
		logger.Warnf("Could not link Xcode toolchain bundle at %s: %s", linkPath, err)

		return
	}

	logger.Infof("Installed Xcode toolchain bundle at %s (linked under %s)", installPath, linkPath)
}

// uninstallXcodeToolchain removes the bundle and discovery symlink.
// Best-effort: deactivate must not block on a stale toolchain.
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
