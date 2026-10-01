// Package xcode_app exposes the public API for `bitrise-build-cache xcode-app
// enable / disable`, which routes Xcode.app IDE builds through Bitrise's
// xcelerate-proxy for remote CAS.
package xcode_app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime"

	"github.com/bitrise-io/go-utils/v2/log"

	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

const darwinGOOS = "darwin"

// ErrUnsupportedPlatform is returned by Enable/Disable on non-macOS hosts.
var ErrUnsupportedPlatform = errors.New("xcode-app is only supported on macOS")

// ErrXcelerateNotConfigured is returned by Enable when the xcelerate config is
// missing — the caller must run `activate xcode` first to obtain a proxy
// socket path.
var ErrXcelerateNotConfigured = errors.New("xcelerate config not found — run `bitrise-build-cache activate xcode` first")

// Activator drives Xcode.app enable/disable. Nil fields fall back to
// production defaults; tests inject fakes.
type Activator struct {
	Logger         log.Logger
	Envs           map[string]string
	OsProxy        utils.OsProxy
	DecoderFactory utils.DecoderFactory
	Launchctl      xa.LaunchctlClient
	XcodeChecker   xa.XcodeProcessChecker
}

// EnableResult reports what Enable actually did — useful for CLI output.
type EnableResult struct {
	XCConfigPath         string
	LaunchAgentPlistPath string
	PreviousXCConfigPath string
	XcelerateProxySocket string
	RunningXcodePIDs     []int
}

// DisableResult reports what Disable actually did — useful for CLI output.
type DisableResult struct {
	XCConfigRemoved    bool
	LaunchAgentRemoved bool
	RunningXcodePIDs   []int
}

// LinkResult / UnlinkResult are returned by Link / Unlink for CLI output.
type LinkResult struct {
	ModifiedXCConfigs []string
	CreatedSiblings   []string
	RunningXcodePIDs  []int
}

type UnlinkResult struct {
	ModifiedXCConfigs []string
	RemovedSiblings   []string
	WarnBaseRefs      []string
	RunningXcodePIDs  []int
}

// Enable writes the override xcconfig, installs the LaunchAgent, and points
// XCODE_XCCONFIG_FILE at the override via launchctl setenv.
//
// On partial failure the caller can run Disable to clean up; each teardown
// step swallows already-gone.
func (a *Activator) Enable(ctx context.Context) (EnableResult, error) {
	if runtime.GOOS != darwinGOOS {
		return EnableResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	logger := a.logger()

	logger.TInfof("Enabling Xcode.app IDE build-cache override")

	cfg, err := xceleratconfig.ReadConfig(osProxy, a.decoderFactory(), a.envs())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return EnableResult{}, fmt.Errorf("%w: %w", ErrXcelerateNotConfigured, err)
		}

		return EnableResult{}, fmt.Errorf("read xcelerate config: %w", err)
	}

	if cfg.ProxySocketPath == "" {
		return EnableResult{}, errors.New("xcelerate config has empty proxy socket path — re-run `activate xcode`")
	}

	xcconfigPath := xceleratconfig.ResolveXcodeAppOverrideXCConfigPath("", a.envs(), osProxy)

	previous := a.currentLaunchctlXCConfig(ctx, xcconfigPath)
	if previous != "" {
		logger.Infof("Preserving prior XCODE_XCCONFIG_FILE override: %s", previous)
	}

	body, err := xa.Render(cfg.ProxySocketPath, previous)
	if err != nil {
		return EnableResult{}, fmt.Errorf("render override xcconfig: %w", err)
	}

	if err := osProxy.MkdirAll(xceleratconfig.DirPath(osProxy), 0o755); err != nil {
		return EnableResult{}, fmt.Errorf("mkdir xcelerate dir: %w", err)
	}

	if err := osProxy.WriteFile(xcconfigPath, []byte(body), 0o644); err != nil { //nolint:gosec // xcconfig must be readable by Xcode
		return EnableResult{}, fmt.Errorf("write %s: %w", xcconfigPath, err)
	}
	logger.Debugf("Wrote override xcconfig: %s", xcconfigPath)

	if err := a.Launchctl.Setenv(ctx, xa.XCConfigEnvVar, xcconfigPath); err != nil {
		return EnableResult{}, fmt.Errorf("set XCODE_XCCONFIG_FILE: %w", err)
	}
	logger.Debugf("launchctl setenv %s=%s", xa.XCConfigEnvVar, xcconfigPath)

	home, err := osProxy.UserHomeDir()
	if err != nil {
		return EnableResult{}, fmt.Errorf("resolve home dir: %w", err)
	}

	plistPath, err := xa.WriteSetenvAgent(osProxy, home, xcconfigPath)
	if err != nil {
		return EnableResult{}, fmt.Errorf("write LaunchAgent: %w", err)
	}
	logger.Debugf("Wrote LaunchAgent plist: %s", plistPath)

	if err := a.Launchctl.Bootstrap(ctx, plistPath); err != nil {
		return EnableResult{}, fmt.Errorf("bootstrap LaunchAgent: %w", err)
	}
	logger.Infof("Bootstrapped LaunchAgent so XCODE_XCCONFIG_FILE persists across logins")

	pids := a.runningXcodePIDs(ctx, logger)

	return EnableResult{
		XCConfigPath:         xcconfigPath,
		LaunchAgentPlistPath: plistPath,
		PreviousXCConfigPath: previous,
		XcelerateProxySocket: cfg.ProxySocketPath,
		RunningXcodePIDs:     pids,
	}, nil
}

// Disable is idempotent against "never enabled" / "already disabled".
func (a *Activator) Disable(ctx context.Context) (DisableResult, error) {
	if runtime.GOOS != darwinGOOS {
		return DisableResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	logger := a.logger()

	logger.TInfof("Disabling Xcode.app IDE build-cache override")

	home, err := osProxy.UserHomeDir()
	if err != nil {
		return DisableResult{}, fmt.Errorf("resolve home dir: %w", err)
	}

	plistPath := paths.FromHome(home).XcodeAppSetenvAgentPlistFile()

	if err := a.Launchctl.Bootout(ctx, plistPath); err != nil {
		return DisableResult{}, fmt.Errorf("bootout LaunchAgent: %w", err)
	}
	logger.Debugf("Booted out LaunchAgent: %s", plistPath)

	removedPlist, err := xa.RemoveSetenvAgent(osProxy, home)
	if err != nil {
		return DisableResult{}, fmt.Errorf("remove LaunchAgent plist: %w", err)
	}
	logger.Debugf("Removed LaunchAgent plist: %s", removedPlist)

	if err := a.Launchctl.Unsetenv(ctx, xa.XCConfigEnvVar); err != nil {
		return DisableResult{}, fmt.Errorf("unset XCODE_XCCONFIG_FILE: %w", err)
	}
	logger.Debugf("Unset %s via launchctl", xa.XCConfigEnvVar)

	xcconfigPath := xceleratconfig.ResolveXcodeAppOverrideXCConfigPath("", a.envs(), osProxy)

	result := DisableResult{
		LaunchAgentRemoved: true,
	}

	switch err := osProxy.Remove(xcconfigPath); {
	case err == nil:
		result.XCConfigRemoved = true
		logger.Debugf("Removed override xcconfig: %s", xcconfigPath)
	case errors.Is(err, fs.ErrNotExist):
	default:
		return result, fmt.Errorf("remove %s: %w", xcconfigPath, err)
	}

	result.RunningXcodePIDs = a.runningXcodePIDs(ctx, logger)

	return result, nil
}

// Link wires each XCBuildConfiguration in the referenced project(s) to the
// override xcconfig via baseConfigurationReference / `#include?`. Required on
// Xcode 27+ IDE builds — `launchctl setenv XCODE_XCCONFIG_FILE` does not
// propagate to SwiftBuild there.
func (a *Activator) Link(ctx context.Context, projectPath string) (LinkResult, error) {
	if runtime.GOOS != darwinGOOS {
		return LinkResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	logger := a.logger()

	overridePath := xceleratconfig.ResolveXcodeAppOverrideXCConfigPath("", a.envs(), osProxy)

	result, err := xa.Link(osProxy, xa.LinkParams{ProjectPath: projectPath, OverrideXCConfigPath: overridePath})
	if err != nil {
		return LinkResult{}, fmt.Errorf("link %s: %w", projectPath, err)
	}

	return LinkResult{
		ModifiedXCConfigs: result.ModifiedXCConfigs,
		CreatedSiblings:   result.CreatedSiblings,
		RunningXcodePIDs:  a.runningXcodePIDs(ctx, logger),
	}, nil
}

// Unlink strips the marker-fenced `#include?` block Link added, and removes
// any sibling xcconfig Link created when its only remaining content is the
// marker block. `baseConfigurationReference` set by Link is NOT reverted:
// pbxproj gives us no way to distinguish "user had this before" from "Link set
// it".
func (a *Activator) Unlink(ctx context.Context, projectPath string) (UnlinkResult, error) {
	if runtime.GOOS != darwinGOOS {
		return UnlinkResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	logger := a.logger()

	result, err := xa.Unlink(osProxy, xa.LinkParams{ProjectPath: projectPath})
	if err != nil {
		return UnlinkResult{}, fmt.Errorf("unlink %s: %w", projectPath, err)
	}

	return UnlinkResult{
		ModifiedXCConfigs: result.ModifiedXCConfigs,
		RemovedSiblings:   result.RemovedSiblings,
		WarnBaseRefs:      result.WarnBaseRefs,
		RunningXcodePIDs:  a.runningXcodePIDs(ctx, logger),
	}, nil
}

// Private ---------------------------------------------------------------

func (a *Activator) osProxy() utils.OsProxy {
	if a.OsProxy != nil {
		return a.OsProxy
	}

	return utils.DefaultOsProxy{}
}

func (a *Activator) logger() log.Logger {
	if a.Logger != nil {
		return a.Logger
	}

	return log.NewLogger()
}

func (a *Activator) decoderFactory() utils.DecoderFactory {
	if a.DecoderFactory != nil {
		return a.DecoderFactory
	}

	return utils.DefaultDecoderFactory{}
}

func (a *Activator) envs() map[string]string {
	if a.Envs != nil {
		return a.Envs
	}

	return utils.AllEnvs()
}

// currentLaunchctlXCConfig returns the current launchctl-scoped
// XCODE_XCCONFIG_FILE. If it already points at our override, treat that as
// "no prior override" so a repeat Enable does not self-chain into an infinite
// #include cycle.
func (a *Activator) currentLaunchctlXCConfig(ctx context.Context, ownPath string) string {
	current, err := a.Launchctl.Getenv(ctx, xa.XCConfigEnvVar)
	if err != nil || current == "" || current == ownPath {
		return ""
	}

	return current
}

func (a *Activator) runningXcodePIDs(ctx context.Context, logger log.Logger) []int {
	checker := a.XcodeChecker
	if checker == nil {
		checker = xa.DefaultXcodeChecker{}
	}

	pids, err := checker.RunningPIDs(ctx)
	if err != nil {
		logger.Debugf("Could not detect running Xcode: %s", err)

		return nil
	}

	return pids
}
