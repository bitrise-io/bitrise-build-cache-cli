package xcode_app

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

const darwinGOOS = "darwin"

var ErrUnsupportedPlatform = errors.New("xcode link/unlink is only supported on macOS")

// Activator drives Xcode.app link/unlink. Nil fields fall back to production
// defaults; tests inject fakes.
type Activator struct {
	Logger  log.Logger
	Envs    map[string]string
	OsProxy utils.OsProxy
}

// LinkOptions carries opt-in link toggles beyond the project path.
type LinkOptions struct {
	// PostBuildScript injects a scheme PostActions ExecutionAction that invokes
	// `bitrise-build-cache xcelerate flush-session` when a build completes.
	PostBuildScript bool
}

type (
	LinkResult   = xa.LinkResult
	UnlinkResult = xa.UnlinkResult
)

// Link wires each XCBuildConfiguration in the referenced project(s) to the
// override xcconfig written by `activate xcode`. Required on Xcode 27+ IDE
// builds — Xcode no longer propagates the `XCODE_XCCONFIG_FILE` user-env
// override to SwiftBuild, so the project is the only route into the IDE's
// compilation tasks.
func (a *Activator) Link(ctx context.Context, projectPath string) (LinkResult, error) {
	return a.LinkWithOptions(ctx, projectPath, LinkOptions{})
}

// LinkWithOptions is Link plus opt-in extras (post-build script, future knobs).
func (a *Activator) LinkWithOptions(_ context.Context, projectPath string, opts LinkOptions) (LinkResult, error) {
	if runtime.GOOS != darwinGOOS {
		return LinkResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	logger := a.logger()

	logger.TInfof("Linking %s to the Xcode.app build-cache override", projectPath)

	overridePath, err := xa.ResolveOverrideXCConfigPath("", a.envs(), osProxy)
	if err != nil {
		return LinkResult{}, fmt.Errorf("resolve override xcconfig path: %w", err)
	}
	logger.Debugf("Override xcconfig: %s", overridePath)

	params := xa.LinkParams{ProjectPath: projectPath, OverrideXCConfigPath: overridePath}
	if opts.PostBuildScript {
		cli, err := osProxy.Executable()
		if err != nil {
			return LinkResult{}, fmt.Errorf("resolve CLI binary path: %w", err)
		}
		params.PostBuildScript = true
		params.CLIBinaryPath = cli
		logger.Debugf("Post-build-script CLI binary: %s", cli)
	}

	result, err := xa.Link(osProxy, params)
	if err != nil {
		return LinkResult{}, fmt.Errorf("link %s: %w", projectPath, err)
	}

	for _, f := range result.ModifiedXCConfigs {
		logger.Debugf("Appended include block: %s", f)
	}
	for _, f := range result.CreatedSiblings {
		logger.Debugf("Created sibling xcconfig: %s", f)
	}
	for _, f := range result.ModifiedSchemes {
		logger.Debugf("Injected scheme post-build action: %s", f)
	}

	return result, nil
}

// Unlink strips Link's `#include?` block and removes any sibling xcconfig it
// created. `baseConfigurationReference` set by Link is NOT reverted: pbxproj
// offers no way to distinguish user-set from link-set.
func (a *Activator) Unlink(_ context.Context, projectPath string) (UnlinkResult, error) {
	if runtime.GOOS != darwinGOOS {
		return UnlinkResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	logger := a.logger()

	logger.TInfof("Unlinking %s from the Xcode.app build-cache override", projectPath)

	result, err := xa.Unlink(osProxy, xa.LinkParams{ProjectPath: projectPath})
	if err != nil {
		return UnlinkResult{}, fmt.Errorf("unlink %s: %w", projectPath, err)
	}

	for _, f := range result.ModifiedXCConfigs {
		logger.Debugf("Stripped include block: %s", f)
	}
	for _, f := range result.RemovedSiblings {
		logger.Debugf("Removed sibling xcconfig: %s", f)
	}
	for _, f := range result.ModifiedSchemes {
		logger.Debugf("Removed scheme post-build action: %s", f)
	}

	return result, nil
}

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

func (a *Activator) envs() map[string]string {
	if a.Envs != nil {
		return a.Envs
	}

	return utils.AllEnvs()
}
