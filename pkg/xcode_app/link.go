package xcode_app

import (
	"context"
	"fmt"
	"runtime"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

// LinkResult mirrors the internal shape so callers don't cross the package
// boundary to read it.
type LinkResult struct {
	BridgeFiles          []string
	AlreadyLinked        []string
	OverrideXCConfigPath string
}

type UnlinkResult struct {
	RemovedBridgeFiles []string
	MissingBridgeFiles []string
}

// Link writes a per-project bridge xcconfig next to each referenced
// .xcodeproj. The bridge `#include`s the user-global override xcconfig
// managed by `xcode-app enable`. Idempotent.
func (a *Activator) Link(_ context.Context, projectPath string) (LinkResult, error) {
	if runtime.GOOS != darwinGOOS {
		return LinkResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()
	overridePath := xcelerate.XcodeAppOverrideXCConfigFile(osProxy)

	internalResult, err := xa.Link(osProxy, xa.LinkParams{
		ProjectPath:          projectPath,
		OverrideXCConfigPath: overridePath,
	})
	if err != nil {
		return LinkResult{}, fmt.Errorf("link %s: %w", projectPath, err)
	}

	return LinkResult{
		BridgeFiles:          internalResult.BridgeFiles,
		AlreadyLinked:        internalResult.AlreadyLinked,
		OverrideXCConfigPath: overridePath,
	}, nil
}

// Unlink removes the per-project bridge xcconfig for each referenced
// .xcodeproj. Idempotent — a missing bridge is not an error.
func (a *Activator) Unlink(_ context.Context, projectPath string) (UnlinkResult, error) {
	if runtime.GOOS != darwinGOOS {
		return UnlinkResult{}, ErrUnsupportedPlatform
	}

	osProxy := a.osProxy()

	internalResult, err := xa.Unlink(osProxy, xa.LinkParams{ProjectPath: projectPath})
	if err != nil {
		return UnlinkResult{}, fmt.Errorf("unlink %s: %w", projectPath, err)
	}

	return UnlinkResult{
		RemovedBridgeFiles: internalResult.RemovedBridgeFiles,
		MissingBridgeFiles: internalResult.MissingBridgeFiles,
	}, nil
}
