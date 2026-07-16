package xcode_app

import (
	"context"
	"fmt"
	"runtime"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

// LinkResult mirrors the internal shape so callers don't cross the package
// boundary to read it.
type LinkResult struct {
	// ModifiedXCConfigs are project xcconfig files we appended a Bitrise cache
	// include to (in-place mode).
	ModifiedXCConfigs []string

	// AlreadyLinked lists xcconfigs that already had an up-to-date include.
	AlreadyLinked []string

	// BridgeFiles are sibling bridge xcconfigs written when the project has no
	// in-tree xcconfig files (fallback mode).
	BridgeFiles []string

	// OverrideXCConfigPath is the absolute path the include line points at.
	OverrideXCConfigPath string
}

type UnlinkResult struct {
	// ModifiedXCConfigs are project xcconfigs whose Bitrise include we stripped.
	ModifiedXCConfigs []string

	// RemovedBridgeFiles are sibling bridge xcconfigs we deleted.
	RemovedBridgeFiles []string

	// MissingBridgeFiles are sibling bridges the state said we wrote but were
	// already absent — reported so the CLI can inform the user.
	MissingBridgeFiles []string

	// NoOp is true when unlink had nothing to change.
	NoOp bool
}

// Link wires each referenced .xcodeproj to the override xcconfig. Prefers
// appending `#include? "<override>"` to existing project xcconfig files so
// cache engages automatically after the next Xcode build; falls back to a
// sibling bridge xcconfig only when the project has no in-tree xcconfigs.
// Idempotent.
func (a *Activator) Link(_ context.Context, projectPath string) (LinkResult, error) {
	if runtime.GOOS != darwinGOOS {
		return LinkResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	overridePath := xcelerate.XcodeAppOverrideXCConfigFile(osProxy)

	home, err := osProxy.UserHomeDir()
	if err != nil {
		return LinkResult{}, fmt.Errorf("resolve home dir: %w", err)
	}
	stateDir := paths.FromHome(home).LinkedProjectsDir()

	internalResult, err := xa.Link(osProxy, xa.LinkParams{
		ProjectPath:          projectPath,
		OverrideXCConfigPath: overridePath,
		StateDir:             stateDir,
	})
	if err != nil {
		return LinkResult{}, fmt.Errorf("link %s: %w", projectPath, err)
	}

	return LinkResult{
		ModifiedXCConfigs:    internalResult.ModifiedXCConfigs,
		AlreadyLinked:        internalResult.AlreadyLinked,
		BridgeFiles:          internalResult.BridgeFiles,
		OverrideXCConfigPath: overridePath,
	}, nil
}

// Unlink reverts every edit `Link` made for the referenced project(s).
// Idempotent — nothing to revert reports NoOp.
func (a *Activator) Unlink(_ context.Context, projectPath string) (UnlinkResult, error) {
	if runtime.GOOS != darwinGOOS {
		return UnlinkResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()

	home, err := osProxy.UserHomeDir()
	if err != nil {
		return UnlinkResult{}, fmt.Errorf("resolve home dir: %w", err)
	}
	stateDir := paths.FromHome(home).LinkedProjectsDir()

	internalResult, err := xa.Unlink(osProxy, xa.LinkParams{
		ProjectPath: projectPath,
		StateDir:    stateDir,
	})
	if err != nil {
		return UnlinkResult{}, fmt.Errorf("unlink %s: %w", projectPath, err)
	}

	return UnlinkResult{
		ModifiedXCConfigs:  internalResult.ModifiedXCConfigs,
		RemovedBridgeFiles: internalResult.RemovedBridgeFiles,
		MissingBridgeFiles: internalResult.MissingBridgeFiles,
		NoOp:               internalResult.NoOp,
	}, nil
}
