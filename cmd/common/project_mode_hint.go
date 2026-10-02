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
// gate.
func PrintOptInGateHintIfGated(logger log.Logger, osProxy utils.OsProxy, projectModeFlag string) {
	if logger == nil || osProxy == nil {
		return
	}

	mode := resolvedProjectMode(osProxy, projectModeFlag)
	if mode != machineconfig.ModeOptIn {
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

// resolvedProjectMode prefers the explicit flag when valid, falling back to the
// stored machine config and finally to the default.
func resolvedProjectMode(osProxy utils.OsProxy, flag string) machineconfig.Mode {
	if flag != "" {
		if err := machineconfig.ValidateProjectMode(flag); err == nil {
			return machineconfig.Mode(flag)
		}
	}

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
