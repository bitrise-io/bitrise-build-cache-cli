// Package xcode_app drives Xcode.app (GUI) build-cache enable/disable via launchctl + an override xcconfig. macOS-only.
package xcode_app

import (
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

const OverrideXCConfigFileName = "xcode-app.xcconfig"

const StateFileName = "xcode-app-state.json"

func OverrideXCConfigPath(osProxy utils.OsProxy) string {
	return filepath.Join(xcelerate.DirPath(osProxy), OverrideXCConfigFileName)
}

func StateFilePath(osProxy utils.OsProxy) string {
	return filepath.Join(xcelerate.DirPath(osProxy), StateFileName)
}
