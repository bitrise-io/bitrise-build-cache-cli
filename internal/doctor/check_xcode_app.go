package doctor

import (
	"context"
	"fmt"
	"os"
	"runtime"

	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

func (d *Doctor) xcodeAppCheck() Check {
	return Check{
		Name: "xcode-app-override",
		Diagnose: func(_ context.Context) Result {
			if !d.toolActivated(toolconfig.Xcelerate) {
				return Result{State: StateOK, Detail: "skipped (xcode not activated)"}
			}

			if runtime.GOOS != "darwin" {
				return Result{State: StateOK, Detail: "skipped (macOS only)"}
			}

			osProxy := d.osProxy()
			overridePath, err := xa.ResolveOverrideXCConfigPath("", d.Envs, osProxy)
			if err != nil {
				return Result{State: StateWarn, Detail: fmt.Sprintf("resolve override xcconfig path: %s", err)}
			}
			socketPath := xceleratconfig.ResolveProxySocketPath("", d.Envs, osProxy)

			return diagnoseXcodeAppOverride(overridePath, socketPath, osProxy)
		},
	}
}

// diagnoseXcodeAppOverride reports whether the override xcconfig written by
// `activate xcode` is in place and whether the proxy socket is live.
func diagnoseXcodeAppOverride(overridePath, socketPath string, osProxy utils.OsProxy) Result {
	overrideExists := xcodeAppFileExists(overridePath)
	socketExists := xcodeAppSocketExists(osProxy, socketPath)

	switch {
	case !overrideExists:
		return Result{State: StateWarn, Detail: fmt.Sprintf("override xcconfig missing at %s — re-run `bitrise-build-cache activate xcode`", overridePath)}
	case !socketExists:
		return Result{State: StateWarn, Detail: fmt.Sprintf("override xcconfig present at %s but proxy socket %s is not live — start it with `bitrise-build-cache xcelerate start-proxy`", overridePath, socketPath)}
	default:
		return Result{State: StateOK, Detail: fmt.Sprintf("ok (%s, proxy socket %s)", overridePath, socketPath)}
	}
}

func xcodeAppFileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// xcodeAppSocketExists returns true when the proxy socket exists on disk. Using
// Stat (rather than a connect probe) matches `xcelerateProxyCheck` and avoids
// coupling the doctor's xcode-app leg to proxy liveness it already reports.
func xcodeAppSocketExists(osProxy utils.OsProxy, socketPath string) bool {
	_, err := osProxy.Stat(socketPath)

	return err == nil
}
