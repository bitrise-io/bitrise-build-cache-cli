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
		if errors.Is(err, fs.ErrNotExist) {
			return Result{State: StateWarn, Detail: fmt.Sprintf("%s missing; multi-Xcode auto-follow disabled", paths.XcodeSelectLinkPath)}
		}

		return Result{State: StateWarn, Detail: fmt.Sprintf("lstat %s: %s", paths.XcodeSelectLinkPath, err)}
	}

	anchorTarget, _ := osProxy.Readlink(paths.XcodeSelectLinkPath)

	if _, err := osProxy.Stat(paths.XcodeSelectLinkPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Result{State: StateWarn, Detail: fmt.Sprintf("%s is dangling (target %s does not exist)", paths.XcodeSelectLinkPath, anchorTarget)}
		}

		return Result{State: StateWarn, Detail: fmt.Sprintf("stat %s: %s", paths.XcodeSelectLinkPath, err)}
	}

	pluginPath := paths.XcodeSelectLinkCASPluginPath()
	if _, err := osProxy.Stat(pluginPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Result{State: StateWarn, Detail: fmt.Sprintf("CAS plugin missing at %s (anchor %s -> %s)", pluginPath, paths.XcodeSelectLinkPath, anchorTarget)}
		}

		return Result{State: StateWarn, Detail: fmt.Sprintf("stat %s: %s", pluginPath, err)}
	}

	return Result{State: StateOK, Detail: fmt.Sprintf("ok (anchor %s -> %s)", paths.XcodeSelectLinkPath, anchorTarget)}
}
