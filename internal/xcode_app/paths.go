// Package xcode_app implements the Xcode.app (GUI) build-cache enablement
// flow backing the `bitrise-build-cache xcode-app enable/disable` subcommands.
//
// Mechanism: write an override xcconfig under ~/.bitrise-xcelerate/ with the
// COMPILATION_CACHE_* keys + the proxy socket path, then `launchctl setenv
// XCODE_XCCONFIG_FILE` so the Xcode.app build planner picks it up. A
// LaunchAgent re-runs the setenv on login so the override survives logout.
//
// macOS-only — `launchctl` and Xcode.app don't exist on Linux. The cmd-layer
// gate-keeps that.
package xcode_app

import (
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

// OverrideXCConfigFileName is the basename of the xcconfig we drop into
// ~/.bitrise-xcelerate/. Kept distinct from `config.json` (the xcelerate CLI
// config) so the two files never collide.
const OverrideXCConfigFileName = "xcode-app.xcconfig"

// StateFileName captures the previous XCODE_XCCONFIG_FILE value at enable
// time so disable can restore it. Lives next to the xcconfig.
const StateFileName = "xcode-app-state.json"

// OverrideXCConfigPath returns the absolute path of the override xcconfig
// we manage under ~/.bitrise-xcelerate/.
func OverrideXCConfigPath(osProxy utils.OsProxy) string {
	return filepath.Join(xcelerate.DirPath(osProxy), OverrideXCConfigFileName)
}

// StateFilePath returns the absolute path of the disable-restore state file
// under ~/.bitrise-xcelerate/.
func StateFilePath(osProxy utils.OsProxy) string {
	return filepath.Join(xcelerate.DirPath(osProxy), StateFileName)
}
