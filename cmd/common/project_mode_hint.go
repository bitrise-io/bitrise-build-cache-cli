package common

import (
	"github.com/bitrise-io/go-utils/v2/log"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// PrintOptInGateHintIfGated prints a one-shot hint when the current directory
// would be gated by project-mode=opt-in but no marker is found. Scripted
// activate calls would otherwise skip cache silently; the hint tells the user
// how to opt in. All lookup failures are swallowed — this is a nudge, never a
// gate. Call after PersistProjectMode so the machine config already reflects
// any --project-mode flag.
func PrintOptInGateHintIfGated(logger log.Logger, osProxy utils.OsProxy) {
	if logger == nil || osProxy == nil {
		return
	}

	if resolvedProjectMode(osProxy) != machineconfig.ModeOptIn {
		return
	}

	cwd, err := osProxy.Getwd()
	if err != nil {
		return
	}

	found, _, err := machineconfig.FindMarker(cwd, osProxy)
	if err != nil || found {
		return
	}

	logger.Printf("[!] Local Build Cache is in opt-in mode but %s is not opted in.", cwd)
	logger.Printf("    Cache will stay inactive here until you run:")
	logger.Printf("      bitrise-build-cache project enable")
	logger.Printf("    (run from the project root; creates %s)", paths.ProjectMarkerFilename)
}

// resolvedProjectMode reads the stored machine config and falls back to the
// default on any failure.
func resolvedProjectMode(osProxy utils.OsProxy) machineconfig.Mode {
	p, err := paths.Default()
	if err != nil {
		return machineconfig.ModeAlways
	}

	cfg, err := machineconfig.Read(osProxy, p, nil)
	if err != nil {
		return machineconfig.ModeAlways
	}

	return machineconfig.ResolvedProjectMode(cfg)
}
