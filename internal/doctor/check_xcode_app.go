package doctor

import (
	"context"
	"fmt"
	"os"
	"runtime"

	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

// XCodeAppLaunchctlGetter reads the launchctl-scoped value of a variable.
// Injectable so tests do not shell out to /bin/launchctl.
type XCodeAppLaunchctlGetter interface {
	Getenv(ctx context.Context, key string) (string, error)
}

func (d *Doctor) xcodeAppCheck() Check {
	return Check{
		Name: "xcode-app-override",
		Diagnose: func(ctx context.Context) Result {
			if !d.toolActivated(toolconfig.Xcelerate) {
				return Result{State: StateOK, Detail: "skipped (xcode not activated)"}
			}

			if runtime.GOOS != "darwin" {
				return Result{State: StateOK, Detail: "skipped (macOS only)"}
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return Result{State: StateError, Detail: "resolve home dir: " + err.Error()}
			}

			p := paths.FromHome(home)
			overridePath := xceleratconfig.ResolveXcodeAppOverrideXCConfigPath("", d.Envs, d.osProxy())
			plistPath := p.XcodeAppSetenvAgentPlistFile()

			envValue, envErr := d.launchctlGetter().Getenv(ctx, xa.XCConfigEnvVar)

			return diagnoseXcodeAppOverride(overridePath, plistPath, envValue, envErr)
		},
	}
}

func (d *Doctor) launchctlGetter() XCodeAppLaunchctlGetter {
	if d.LaunchctlGetter != nil {
		return d.LaunchctlGetter
	}

	return xa.LaunchctlClient{}
}

func diagnoseXcodeAppOverride(overridePath, plistPath, envValue string, envErr error) Result {
	overrideExists := xcodeAppFileExists(overridePath)
	plistExists := xcodeAppFileExists(plistPath)

	pointsAtUs := envValue == overridePath

	switch {
	case envErr != nil:
		return Result{State: StateWarn, Detail: "launchctl getenv failed: " + envErr.Error()}
	case envValue == "" && !overrideExists && !plistExists:
		return Result{State: StateOK, Detail: "not enabled (no launchctl override, no plist, no xcconfig)"}
	case pointsAtUs && overrideExists && plistExists:
		return Result{State: StateOK, Detail: fmt.Sprintf("enabled (%s, LaunchAgent %s)", overridePath, plistPath)}
	case pointsAtUs && !overrideExists:
		return Result{State: StateWarn, Detail: fmt.Sprintf("XCODE_XCCONFIG_FILE points at %s but that file is missing — re-run `xcode-app enable`", overridePath)}
	case pointsAtUs && !plistExists:
		return Result{State: StateWarn, Detail: fmt.Sprintf("XCODE_XCCONFIG_FILE points at our override but the LaunchAgent plist (%s) is missing — override will vanish at next logout", plistPath)}
	case envValue != "" && !pointsAtUs && overrideExists:
		return Result{State: StateWarn, Detail: fmt.Sprintf("XCODE_XCCONFIG_FILE=%s does not point at our override %s — re-run `xcode-app enable` to chain it in", envValue, overridePath)}
	case overrideExists && !pointsAtUs:
		return Result{State: StateWarn, Detail: fmt.Sprintf("override xcconfig present at %s but XCODE_XCCONFIG_FILE is unset — re-run `xcode-app enable`", overridePath)}
	}

	return Result{State: StateOK, Detail: "not enabled"}
}

func xcodeAppFileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
