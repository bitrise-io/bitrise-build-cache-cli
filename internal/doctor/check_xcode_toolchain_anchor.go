package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func (d *Doctor) xcodeToolchainAnchorCheck() Check {
	return Check{
		Name: "xcode-toolchain-anchor",
		Diagnose: func(_ context.Context) Result {
			if !d.toolActivated(toolconfig.Xcelerate) {
				return Result{State: StateOK, Detail: "skipped (xcode not activated)"}
			}

			if runtime.GOOS != utils.DarwinGOOS {
				return Result{State: StateOK, Detail: "skipped (macOS only)"}
			}

			return diagnoseXcodeToolchainAnchor(d.osProxy())
		},
	}
}

func diagnoseXcodeToolchainAnchor(osProxy utils.OsProxy) Result {
	if _, err := osProxy.Lstat(paths.XcodeSelectLinkPath); err != nil {
		return anchorStatWarn("lstat", paths.XcodeSelectLinkPath, err,
			fmt.Sprintf("%s missing; multi-Xcode auto-follow disabled", paths.XcodeSelectLinkPath))
	}

	anchorTarget, _ := osProxy.Readlink(paths.XcodeSelectLinkPath)

	if _, err := osProxy.Stat(paths.XcodeSelectLinkPath); err != nil {
		return anchorStatWarn("stat", paths.XcodeSelectLinkPath, err,
			fmt.Sprintf("%s is dangling (target %s does not exist)", paths.XcodeSelectLinkPath, anchorTarget))
	}

	pluginPath := paths.XcodeSelectLinkCASPluginPath()
	if _, err := osProxy.Stat(pluginPath); err != nil {
		return anchorStatWarn("stat", pluginPath, err,
			fmt.Sprintf("CAS plugin missing at %s (anchor %s -> %s)", pluginPath, paths.XcodeSelectLinkPath, anchorTarget))
	}

	return Result{State: StateOK, Detail: fmt.Sprintf("ok (anchor %s -> %s)", paths.XcodeSelectLinkPath, anchorTarget)}
}

func anchorStatWarn(op, path string, err error, missingDetail string) Result {
	if errors.Is(err, fs.ErrNotExist) {
		return Result{State: StateWarn, Detail: missingDetail}
	}

	return Result{State: StateWarn, Detail: fmt.Sprintf("%s %s: %s", op, path, err)}
}
