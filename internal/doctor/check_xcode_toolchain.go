package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"howett.net/plist"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

var toolchainUsrBinShims = []string{"swiftc", "clang", "swift"} //nolint:gochecknoglobals // immutable layout data

func (d *Doctor) xcodeToolchainCheck() Check {
	return Check{
		Name: "xcode-toolchain",
		Diagnose: func(_ context.Context) Result {
			if !d.toolActivated(toolconfig.Xcelerate) {
				return Result{State: StateOK, Detail: "skipped (xcode not activated)"}
			}

			if runtime.GOOS != osDarwin {
				return Result{State: StateOK, Detail: "skipped (macOS only)"}
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return Result{State: StateError, Detail: "resolve home dir: " + err.Error()}
			}

			bundlePath := paths.FromHome(home).XcodeToolchainBundleDir()

			return diagnoseXcodeToolchain(bundlePath)
		},
	}
}

func diagnoseXcodeToolchain(bundlePath string) Result {
	if !xcodeAppFileExists(bundlePath) {
		return Result{
			State:  StateWarn,
			Detail: fmt.Sprintf("toolchain bundle missing at %s — re-run `bitrise-build-cache activate xcode`", bundlePath),
		}
	}

	plistPath := filepath.Join(bundlePath, xa.ToolchainInfoPlistFile)
	if err := verifyToolchainPlist(plistPath); err != nil {
		return Result{State: StateWarn, Detail: err.Error()}
	}

	if err := verifyToolchainUsrBin(bundlePath); err != nil {
		return Result{State: StateWarn, Detail: err.Error()}
	}

	sorted := append([]string(nil), toolchainUsrBinShims...)
	sort.Strings(sorted)

	return Result{
		State: StateOK,
		Detail: fmt.Sprintf("ok (bundle at %s, %s shims present)",
			bundlePath, strings.Join(sorted, "/")),
	}
}

func verifyToolchainPlist(plistPath string) error {
	body, err := os.ReadFile(plistPath) //nolint:gosec // path derived from internal layout
	if err != nil {
		return fmt.Errorf("toolchain Info.plist invalid at %s — re-run `bitrise-build-cache activate xcode`", plistPath)
	}

	var raw map[string]interface{}
	if _, err := plist.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("toolchain Info.plist invalid at %s — re-run `bitrise-build-cache activate xcode`", plistPath)
	}

	id, _ := raw["Identifier"].(string)
	if id != xa.ToolchainID {
		return fmt.Errorf("toolchain Info.plist invalid at %s — re-run `bitrise-build-cache activate xcode`", plistPath)
	}

	settings, _ := raw["OverrideBuildSettings"].(map[string]interface{})
	for _, k := range xa.ToolchainOverrideBuildSettingsKeys {
		if _, ok := settings[k]; !ok {
			return fmt.Errorf("toolchain Info.plist out of date — re-run `bitrise-build-cache activate xcode`")
		}
	}

	return nil
}

func verifyToolchainUsrBin(bundlePath string) error {
	binDir := filepath.Join(bundlePath, "usr", "bin")
	for _, name := range toolchainUsrBinShims {
		entryPath := filepath.Join(binDir, name)
		info, err := os.Lstat(entryPath)
		if err != nil {
			return fmt.Errorf("toolchain shim %s missing/non-executable — re-run `bitrise-build-cache activate xcode`", name)
		}
		// A symlink's own mode doesn't carry +x; stat through to the target.
		if info.Mode()&os.ModeSymlink != 0 {
			if _, err := os.Stat(entryPath); err != nil {
				return fmt.Errorf("toolchain shim %s missing/non-executable — re-run `bitrise-build-cache activate xcode`", name)
			}

			continue
		}
		if info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("toolchain shim %s missing/non-executable — re-run `bitrise-build-cache activate xcode`", name)
		}
	}

	return nil
}
