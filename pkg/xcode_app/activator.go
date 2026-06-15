// Package xcode_app exposes the public API for the `bitrise-build-cache
// xcode-app enable / disable` flows.
//
// The Activator wires together the building blocks under
// internal/xcode_app and the daemon package so external Go consumers
// (Bitrise steps, custom tooling) can drive Xcode.app cache enablement
// without depending on cobra.
//
// Design follows the project's exported-struct-no-interface convention:
// consumers define their own interfaces for mocking via Go's implicit
// satisfaction.
package xcode_app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/xcode_app"
)

// ErrUnsupportedPlatform is returned by Enable / Disable on non-macOS hosts.
// Xcode.app + launchctl only exist on Darwin.
var ErrUnsupportedPlatform = errors.New("xcode-app is only supported on macOS")

// ErrXcelerateNotConfigured is returned when the xcelerate config file is
// not on disk. enable depends on it for the proxy socket path; the user is
// expected to run `bitrise-build-cache activate xcode` first.
var ErrXcelerateNotConfigured = errors.New("xcelerate config not found — run `bitrise-build-cache activate xcode` first")

// Activator drives `xcode-app enable` / `disable`. All collaborator fields
// are injectable; nil means "use the production default".
type Activator struct {
	// Logger is the user-facing log surface. Required.
	Logger log.Logger

	// Envs is the environment snapshot used to detect a pre-existing
	// XCODE_XCCONFIG_FILE override at enable time. Production callers
	// pass utils.AllEnvs().
	Envs map[string]string

	// OsProxy gates filesystem reads (xcelerate config). nil falls back to
	// utils.DefaultOsProxy.
	OsProxy utils.OsProxy

	// DecoderFactory builds the JSON decoder for the xcelerate config.
	// nil falls back to utils.DefaultDecoderFactory.
	DecoderFactory utils.DecoderFactory

	// Launchctl handles setenv / unsetenv / bootstrap / bootout. Nil falls
	// back to xa.LaunchctlClient{} (real `/bin/launchctl`).
	Launchctl xa.LaunchctlClient

	// XcodeChecker reports whether Xcode is currently running so we can
	// nudge the user to relaunch. Nil falls back to
	// xa.DefaultXcodeChecker{}.
	XcodeChecker xa.XcodeProcessChecker

	// DaemonBackend supervises the xcelerate-proxy service. Nil falls
	// back to daemon.DefaultBackend(). Enable runs Install + Up filtered
	// to the xcelerate-proxy service so the proxy is alive when Xcode.app
	// next dials it.
	DaemonBackend daemon.Backend

	// DaemonPaths is the supervisor paths struct. Nil falls back to
	// daemon.NewPaths() at call time.
	DaemonPaths *daemon.Paths

	// Executable is the path of this CLI binary, used as the supervisor's
	// ProgramArguments[0]. Empty falls back to os.Executable() at call
	// time.
	Executable string
}

// EnableResult describes what Enable did. Used by the CLI command to surface
// concrete next-step prompts (e.g. relaunch Xcode).
type EnableResult struct {
	XCConfigPath         string
	LaunchAgentPlistPath string
	PreviousXCConfigPath string
	XcelerateProxySocket string
	RunningXcodePIDs     []int
}

// DisableResult describes what Disable did.
type DisableResult struct {
	XCConfigRemoved      bool
	LaunchAgentRemoved   bool
	PreviousXCConfigPath string
	RestoredXCConfigPath string
}

// Enable applies the Xcode.app build-cache override. See package doc for the
// mechanism overview.
func (a *Activator) Enable(ctx context.Context) (EnableResult, error) {
	if runtime.GOOS != "darwin" {
		return EnableResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	logger := a.Logger

	cfg, err := xcelerate.ReadConfig(osProxy, a.decoderFactory())
	if err != nil {
		return EnableResult{}, fmt.Errorf("%w: %w", ErrXcelerateNotConfigured, err)
	}

	if cfg.ProxySocketPath == "" {
		return EnableResult{}, fmt.Errorf("xcelerate config has empty proxy socket path — re-run `activate xcode`")
	}

	previous := a.Envs["XCODE_XCCONFIG_FILE"]

	body, err := xa.RenderOverride(cfg.ProxySocketPath, previous)
	if err != nil {
		return EnableResult{}, fmt.Errorf("render override xcconfig: %w", err)
	}

	xcconfigPath := xa.OverrideXCConfigPath(osProxy)

	if err := osProxy.MkdirAll(xcelerate.DirPath(osProxy), 0o755); err != nil {
		return EnableResult{}, fmt.Errorf("mkdir xcelerate dir: %w", err)
	}

	if err := osProxy.WriteFile(xcconfigPath, []byte(body), 0o644); err != nil { //nolint:gosec // xcconfig must be readable by Xcode
		return EnableResult{}, fmt.Errorf("write %s: %w", xcconfigPath, err)
	}

	if err := xa.SaveState(xa.StateFilePath(osProxy), xa.State{PreviousXCConfigPath: previous}); err != nil {
		return EnableResult{}, fmt.Errorf("save state: %w", err)
	}

	if err := a.Launchctl.Setenv(ctx, xa.XCConfigEnvVar, xcconfigPath); err != nil {
		return EnableResult{}, fmt.Errorf("set XCODE_XCCONFIG_FILE: %w", err)
	}

	home, err := osProxy.UserHomeDir()
	if err != nil {
		return EnableResult{}, fmt.Errorf("resolve home dir: %w", err)
	}

	plistPath, err := xa.WriteSetenvAgent(home, xcconfigPath)
	if err != nil {
		return EnableResult{}, fmt.Errorf("write LaunchAgent: %w", err)
	}

	if err := a.Launchctl.Bootstrap(ctx, plistPath); err != nil {
		return EnableResult{}, fmt.Errorf("bootstrap LaunchAgent: %w", err)
	}

	if err := a.installAndStartProxy(ctx); err != nil {
		logger.Warnf("Could not start xcelerate-proxy daemon: %s. Run `bitrise-build-cache daemon install + up` manually.", err)
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
	}, nil
}

// Disable reverses what Enable did. Safe to run repeatedly — every step is
// idempotent against "never enabled" / "already disabled".
func (a *Activator) Disable(ctx context.Context) (DisableResult, error) {
	if runtime.GOOS != "darwin" {
		return DisableResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()

	home, err := osProxy.UserHomeDir()
	if err != nil {
		return DisableResult{}, fmt.Errorf("resolve home dir: %w", err)
	}

	plistPath := xa.SetenvAgentPlistPath(home)

	if err := a.Launchctl.Bootout(ctx, plistPath); err != nil {
		return DisableResult{}, fmt.Errorf("bootout LaunchAgent: %w", err)
	}

	if _, err := xa.RemoveSetenvAgent(home); err != nil {
		return DisableResult{}, fmt.Errorf("remove LaunchAgent plist: %w", err)
	}

	state, _, err := xa.LoadState(xa.StateFilePath(osProxy))
	if err != nil {
		return DisableResult{}, fmt.Errorf("load state: %w", err)
	}

	result := DisableResult{
		LaunchAgentRemoved:   true,
		PreviousXCConfigPath: state.PreviousXCConfigPath,
	}

	if state.PreviousXCConfigPath != "" {
		// Restore the user's prior override so their tooling keeps working.
		if err := a.Launchctl.Setenv(ctx, xa.XCConfigEnvVar, state.PreviousXCConfigPath); err != nil {
			return result, fmt.Errorf("restore previous XCODE_XCCONFIG_FILE: %w", err)
		}

		result.RestoredXCConfigPath = state.PreviousXCConfigPath
	} else {
		if err := a.Launchctl.Unsetenv(ctx, xa.XCConfigEnvVar); err != nil {
			return result, fmt.Errorf("unset XCODE_XCCONFIG_FILE: %w", err)
		}
	}

	xcconfigPath := xa.OverrideXCConfigPath(osProxy)
	if err := osProxy.Remove(xcconfigPath); err != nil {
		// Not-exist is fine — already disabled.
		if !errors.Is(err, fs.ErrNotExist) {
			return result, fmt.Errorf("remove %s: %w", xcconfigPath, err)
		}
	} else {
		result.XCConfigRemoved = true
	}

	if err := xa.RemoveState(xa.StateFilePath(osProxy)); err != nil {
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

	svc := xcelerateProxyService()

	if _, err := daemon.Install(ctx, backend, paths, []daemon.Service{svc}, executable); err != nil {
		return fmt.Errorf("daemon install: %w", err)
	}

	if _, err := daemon.Up(ctx, backend, paths, []daemon.Service{svc}); err != nil {
		return fmt.Errorf("daemon up: %w", err)
	}

	return nil
}

// xcelerateProxyService picks the xcelerate-proxy service out of the
// daemon's DefaultServices set. We deliberately don't drive ccache-helper
// from here — Xcode.app builds don't use ccache, and bundling the two would
// surprise C/C++-only users.
func xcelerateProxyService() daemon.Service {
	for _, s := range daemon.DefaultServices() {
		if s.Name == "xcelerate-proxy" {
			return s
		}
	}

	// Defensive — the only way to reach this is if someone removes
	// xcelerate-proxy from daemon.DefaultServices, which would break a
	// lot more than this code path.
	return daemon.Service{Name: "xcelerate-proxy", Args: []string{"xcelerate", "start-proxy"}}
}

// ───── Private ─ DI fallbacks ─────

func (a *Activator) osProxy() utils.OsProxy {
	if a.OsProxy != nil {
		return a.OsProxy
	}

	return utils.DefaultOsProxy{}
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
