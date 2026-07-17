// Package xcode_app exposes the public API for the `bitrise-build-cache xcode-app enable / disable` flows.
package xcode_app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

const darwinGOOS = "darwin"

var ErrUnsupportedPlatform = errors.New("xcode-app is only supported on macOS")

var ErrXcelerateNotConfigured = errors.New("xcelerate config not found — run `bitrise-build-cache activate xcode` first")

// Activator nil fields fall back to production defaults.
type Activator struct {
	Logger         log.Logger
	Envs           map[string]string
	OsProxy        utils.OsProxy
	DecoderFactory utils.DecoderFactory
	Launchctl      xa.LaunchctlClient
	XcodeChecker   xa.XcodeProcessChecker
	DaemonBackend  daemon.Backend
	DaemonPaths    *daemon.Paths
	Executable     string
}

type EnableResult struct {
	XCConfigPath         string
	LaunchAgentPlistPath string
	PreviousXCConfigPath string
	XcelerateProxySocket string
	RunningXcodePIDs     []int
	ProxyStartError      error
}

type DisableResult struct {
	XCConfigRemoved      bool
	LaunchAgentRemoved   bool
	PreviousXCConfigPath string
	RestoredXCConfigPath string
}

// Enable: on partial failure, follow-up Disable cleans up — each teardown step swallows already-gone.
func (a *Activator) Enable(ctx context.Context) (EnableResult, error) {
	if runtime.GOOS != darwinGOOS {
		return EnableResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	logger := a.logger()

	logger.TInfof("Enabling Xcode.app build cache override")

	logger.Debugf("Reading xcelerate config from %s", xcelerate.ConfigFile(osProxy))
	cfg, err := xcelerate.ReadConfig(osProxy, a.decoderFactory())
	if err != nil {
		return EnableResult{}, fmt.Errorf("%w: %w", ErrXcelerateNotConfigured, err)
	}

	if cfg.ProxySocketPath == "" {
		return EnableResult{}, fmt.Errorf("xcelerate config has empty proxy socket path — re-run `activate xcode`")
	}

	xcconfigPath := xcelerate.XcodeAppOverrideXCConfigFile(osProxy)
	statePath := xcelerate.XcodeAppStateFile(osProxy)

	previous := resolvePreviousOverride(logger, a.Envs[xa.XCConfigEnvVar], statePath, xcconfigPath)
	if previous != "" {
		logger.Infof("Preserving prior XCODE_XCCONFIG_FILE override: %s", previous)
	}

	body, err := xa.RenderOverride(cfg.ProxySocketPath, previous)
	if err != nil {
		return EnableResult{}, fmt.Errorf("render override xcconfig: %w", err)
	}

	if err := osProxy.MkdirAll(xcelerate.DirPath(osProxy), 0o755); err != nil {
		return EnableResult{}, fmt.Errorf("mkdir xcelerate dir: %w", err)
	}

	if err := osProxy.WriteFile(xcconfigPath, []byte(body), 0o644); err != nil { //nolint:gosec // xcconfig must be readable by Xcode
		return EnableResult{}, fmt.Errorf("write %s: %w", xcconfigPath, err)
	}
	logger.Debugf("Wrote override xcconfig: %s", xcconfigPath)

	if err := xa.SaveState(statePath, xa.State{PreviousXCConfigPath: previous}); err != nil {
		return EnableResult{}, fmt.Errorf("save state: %w", err)
	}
	logger.Debugf("Saved xcode-app state: %s", statePath)

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

	proxyErr := a.installAndStartProxy(ctx)
	if proxyErr != nil {
		logger.Warnf("Could not start xcelerate-proxy daemon: %s. Run `bitrise-build-cache daemon install + up` manually.", proxyErr)
	} else {
		logger.Infof("Started xcelerate-proxy daemon")
	}

	pids, checkErr := a.xcodeChecker().RunningPIDs(ctx)
	if checkErr != nil {
		logger.Debugf("Could not detect running Xcode: %s", checkErr)
	}

	return EnableResult{
		XCConfigPath:         xcconfigPath,
		LaunchAgentPlistPath: plistPath,
		PreviousXCConfigPath: previous,
		XcelerateProxySocket: cfg.ProxySocketPath,
		RunningXcodePIDs:     pids,
		ProxyStartError:      proxyErr,
	}, nil
}

// Disable is idempotent against "never enabled" / "already disabled".
func (a *Activator) Disable(ctx context.Context) (DisableResult, error) {
	if runtime.GOOS != darwinGOOS {
		return DisableResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	logger := a.logger()

	logger.TInfof("Disabling Xcode.app build cache override")

	home, err := osProxy.UserHomeDir()
	if err != nil {
		return DisableResult{}, fmt.Errorf("resolve home dir: %w", err)
	}

	plistPath := paths.FromHome(home).XcodeAppSetenvAgentPlistFile()

	if err := a.Launchctl.Bootout(ctx, plistPath); err != nil {
		return DisableResult{}, fmt.Errorf("bootout LaunchAgent: %w", err)
	}
	logger.Debugf("Booted out LaunchAgent: %s", plistPath)

	if _, err := xa.RemoveSetenvAgent(osProxy, home); err != nil {
		return DisableResult{}, fmt.Errorf("remove LaunchAgent plist: %w", err)
	}
	logger.Debugf("Removed LaunchAgent plist: %s", plistPath)

	state, _, err := xa.LoadState(xcelerate.XcodeAppStateFile(osProxy))
	if err != nil {
		logger.Warnf("Could not read xcode-app state file (%s) — continuing with best-effort cleanup.", err)

		state = xa.State{}
	}

	result := DisableResult{
		LaunchAgentRemoved:   true,
		PreviousXCConfigPath: state.PreviousXCConfigPath,
	}

	if state.PreviousXCConfigPath != "" {
		if err := a.Launchctl.Setenv(ctx, xa.XCConfigEnvVar, state.PreviousXCConfigPath); err != nil {
			return result, fmt.Errorf("restore previous XCODE_XCCONFIG_FILE: %w", err)
		}

		result.RestoredXCConfigPath = state.PreviousXCConfigPath
		logger.Infof("Restored prior XCODE_XCCONFIG_FILE: %s", state.PreviousXCConfigPath)
	} else {
		if err := a.Launchctl.Unsetenv(ctx, xa.XCConfigEnvVar); err != nil {
			return result, fmt.Errorf("unset XCODE_XCCONFIG_FILE: %w", err)
		}
		logger.Debugf("Unset %s via launchctl", xa.XCConfigEnvVar)
	}

	xcconfigPath := xcelerate.XcodeAppOverrideXCConfigFile(osProxy)
	if err := osProxy.Remove(xcconfigPath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return result, fmt.Errorf("remove %s: %w", xcconfigPath, err)
		}
	} else {
		result.XCConfigRemoved = true
		logger.Debugf("Removed override xcconfig: %s", xcconfigPath)
	}

	if err := xa.RemoveState(xcelerate.XcodeAppStateFile(osProxy)); err != nil {
		return result, fmt.Errorf("remove state file: %w", err)
	}

	return result, nil
}

func (a *Activator) installAndStartProxy(ctx context.Context) error {
	backend, err := a.daemonBackend()
	if err != nil {
		return err
	}

	paths, err := a.daemonPaths()
	if err != nil {
		return err
	}

	executable, err := a.executable()
	if err != nil {
		return err
	}

	svc, err := xcelerateProxyService()
	if err != nil {
		return err
	}

	if _, err := daemon.Install(ctx, backend, paths, []daemon.Service{svc}, executable); err != nil {
		return fmt.Errorf("daemon install: %w", err)
	}

	if _, err := daemon.Up(ctx, backend, paths, []daemon.Service{svc}); err != nil {
		return fmt.Errorf("daemon up: %w", err)
	}

	return nil
}

// resolvePreviousOverride: on self-loop (env == ownPath) fall back to on-disk state so the original prior override survives repeat Enables.
func resolvePreviousOverride(logger log.Logger, envValue, statePath, ownPath string) string {
	if envValue != ownPath {
		return envValue
	}

	existing, found, err := xa.LoadState(statePath)
	if err != nil || !found {
		return ""
	}

	if existing.PreviousXCConfigPath != "" {
		logger.Debugf("XCODE_XCCONFIG_FILE points at our own override — recovered prior override from state: %s", existing.PreviousXCConfigPath)
	}

	return existing.PreviousXCConfigPath
}

// xcelerateProxyService excludes ccache-helper intentionally — Xcode.app builds don't use ccache.
// Errors rather than falling back so this caller and daemon's canonical service set can't silently diverge.
func xcelerateProxyService() (daemon.Service, error) {
	for _, s := range daemon.DefaultServices() {
		if s.Name == "xcelerate-proxy" {
			return s, nil
		}
	}

	return daemon.Service{}, fmt.Errorf("daemon.DefaultServices() no longer includes 'xcelerate-proxy' — refusing to fall back to a stale hardcoded service definition")
}

// ───── Private ─ DI fallbacks ─────

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

func (a *Activator) xcodeChecker() xa.XcodeProcessChecker {
	if a.XcodeChecker != nil {
		return a.XcodeChecker
	}

	return xa.DefaultXcodeChecker{}
}

func (a *Activator) daemonBackend() (daemon.Backend, error) {
	if a.DaemonBackend != nil {
		return a.DaemonBackend, nil
	}

	return daemon.DefaultBackend() //nolint:wrapcheck // sentinel ErrUnsupportedPlatform from daemon is the right error shape
}

func (a *Activator) daemonPaths() (daemon.Paths, error) {
	if a.DaemonPaths != nil {
		return *a.DaemonPaths, nil
	}

	return daemon.NewPaths() //nolint:wrapcheck
}

func (a *Activator) executable() (string, error) {
	if a.Executable != "" {
		return a.Executable, nil
	}

	exe, err := a.osProxy().Executable()
	if err != nil {
		return "", fmt.Errorf("os.Executable: %w", err)
	}

	return exe, nil
}
