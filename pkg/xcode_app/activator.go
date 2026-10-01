// Package xcode_app exposes the public API for `bitrise-build-cache xcode-app
// link / unlink`, which wires a .xcodeproj to the override xcconfig written by
// `activate xcode` so Xcode.app IDE builds route through Bitrise's
// xcelerate-proxy for remote CAS.
package xcode_app

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/bitrise-io/go-utils/v2/log"

	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

const darwinGOOS = "darwin"

// ErrUnsupportedPlatform is returned by Link/Unlink on non-macOS hosts.
var ErrUnsupportedPlatform = errors.New("xcode-app is only supported on macOS")

// Activator drives Xcode.app link/unlink. Nil fields fall back to production
// defaults; tests inject fakes.
type Activator struct {
	Logger  log.Logger
	Envs    map[string]string
	OsProxy utils.OsProxy
}

// LinkResult / UnlinkResult wrap the internal results for the CLI layer.
type LinkResult struct {
	xa.LinkResult
}

type UnlinkResult struct {
	xa.UnlinkResult
}

// Link wires each XCBuildConfiguration in the referenced project(s) to the
// override xcconfig via baseConfigurationReference / `#include?`. Required on
// Xcode 27+ IDE builds — `launchctl setenv XCODE_XCCONFIG_FILE` does not
// propagate to SwiftBuild there.
func (a *Activator) Link(_ context.Context, projectPath string) (LinkResult, error) {
	if runtime.GOOS != darwinGOOS {
		return LinkResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	logger := a.logger()

	logger.TInfof("Linking %s to the Xcode.app build-cache override", projectPath)

	overridePath := xceleratconfig.ResolveXcodeAppOverrideXCConfigPath("", a.envs(), osProxy)
	logger.Debugf("Override xcconfig: %s", overridePath)

	result, err := xa.Link(osProxy, xa.LinkParams{ProjectPath: projectPath, OverrideXCConfigPath: overridePath})
	if err != nil {
		return LinkResult{}, fmt.Errorf("link %s: %w", projectPath, err)
	}

	for _, f := range result.ModifiedXCConfigs {
		logger.Debugf("Appended include block: %s", f)
	}
	for _, f := range result.CreatedSiblings {
		logger.Debugf("Created sibling xcconfig: %s", f)
	}

	if len(result.ModifiedXCConfigs)+len(result.CreatedSiblings) == 0 {
		logger.Infof("Project already linked — no changes to %s", projectPath)
	} else {
		logger.Infof("Linked %s (%d xcconfig(s) modified, %d sibling(s) created)",
			projectPath, len(result.ModifiedXCConfigs), len(result.CreatedSiblings))
	}

	return LinkResult{LinkResult: result}, nil
}

// Unlink strips the marker-fenced `#include?` block Link added, and removes
// any sibling xcconfig Link created when its only remaining content is the
// marker block. `baseConfigurationReference` set by Link is NOT reverted:
// pbxproj gives us no way to distinguish "user had this before" from "Link set
// it".
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

	if len(result.ModifiedXCConfigs)+len(result.RemovedSiblings) == 0 {
		logger.Infof("Nothing to revert for %s", projectPath)
	} else {
		logger.Infof("Unlinked %s (%d xcconfig(s) stripped, %d sibling(s) removed)",
			projectPath, len(result.ModifiedXCConfigs), len(result.RemovedSiblings))
	}

	return UnlinkResult{UnlinkResult: result}, nil
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

func (a *Activator) envs() map[string]string {
	if a.Envs != nil {
		return a.Envs
	}

	return utils.AllEnvs()
}
