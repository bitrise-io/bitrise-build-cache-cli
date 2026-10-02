package interactive

import (
	"charm.land/huh/v2"
	"github.com/bitrise-io/go-utils/v2/log"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// confirmMarker is a seam so tests can drive the opt-in confirm without going
// through the real form.
//
//nolint:gochecknoglobals
var confirmMarker = func(cwd string) (bool, error) {
	confirmed := false
	if err := tui.RunForm(huh.NewGroup(
		huh.NewConfirm().
			Title("No marker found at " + cwd + ". Create .bitrise-build-cache.json here?").
			Description("Without a marker this directory stays outside opt-in scope and the cache won't activate for builds launched here.").
			Affirmative("Yes, opt this project in").
			Negative("No, I'll do it later").
			Value(&confirmed),
	)); err != nil {
		return false, err //nolint:wrapcheck // caller logs it
	}

	return confirmed, nil
}

func projectModePrompt(logger log.Logger) (machineconfig.Mode, error) {
	osProxy := utils.DefaultOsProxy{}

	p, err := paths.Default()
	if err != nil {
		return machineconfig.ModeAlways, err //nolint:wrapcheck // caller wraps
	}

	current, err := machineconfig.Read(osProxy, p, logger)
	if err != nil {
		logger.Warnf("Could not read the machine-wide build-cache config (%v); starting from 'always'.", err)
		current = machineconfig.Config{}
	}

	choice := string(machineconfig.ResolvedProjectMode(current))
	description := "'always' activates cache for every project on this machine.\n" +
		"'opt-in' only activates when a .bitrise-build-cache.json marker file " +
		"is found walking up from the build's working directory."

	if err := tui.RunForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Cache for all projects, or only opted-in ones?").
			Description(description).
			Options(
				huh.NewOption("Always (default)", string(machineconfig.ModeAlways)),
				huh.NewOption("Opt-in via .bitrise-build-cache.json marker", string(machineconfig.ModeOptIn)),
			).
			Value(&choice),
	)); err != nil {
		return machineconfig.ModeAlways, err //nolint:wrapcheck // caller handles tui.ErrAborted
	}

	mode := machineconfig.Mode(choice)
	if mode != current.ProjectMode {
		current.ProjectMode = mode
		if writeErr := machineconfig.Write(current, osProxy, p); writeErr != nil {
			logger.Warnf("Could not persist project scoping choice (%v). Continuing with %q for this run.", writeErr, string(mode))
		}
	}

	if mode == machineconfig.ModeOptIn {
		confirmMarkerForCwd(logger, osProxy)
	}

	return mode, nil
}

// confirmMarkerForCwd asks whether to drop the opt-in marker at the current
// directory, when none is already found walking up from it. Any lookup or
// write failure degrades to a warn-and-continue: this is a convenience step,
// not a gate.
func confirmMarkerForCwd(logger log.Logger, osProxy utils.OsProxy) {
	cwd, err := osProxy.Getwd()
	if err != nil {
		logger.Warnf("Could not resolve the current directory to offer opt-in (%v). Run `bitrise-build-cache project enable` from the project root to opt it in.", err)

		return
	}

	found, _, err := machineconfig.FindMarker(cwd, osProxy)
	if err != nil {
		logger.Warnf("Could not look up the .bitrise-build-cache.json marker (%v). Run `bitrise-build-cache project enable` from the project root to opt it in.", err)

		return
	}
	if found {
		return
	}

	confirmed, promptErr := confirmMarker(cwd)
	if promptErr != nil {
		logger.Warnf("Could not confirm opt-in for %s (%v). Run `bitrise-build-cache project enable` from the project root to opt it in.", cwd, promptErr)

		return
	}
	if !confirmed {
		return
	}

	wrote, ancestor, err := machineconfig.WriteMarkerIfMissing(cwd, osProxy)
	if err != nil {
		logger.Warnf("Could not opt %s in (%v). Run `bitrise-build-cache project enable` from the project root to retry.", cwd, err)

		return
	}
	if ancestor != "" {
		logger.Infof("Marker already covers %s (found at %s).", cwd, ancestor)

		return
	}
	logger.Infof("Wrote .bitrise-build-cache.json at %s.", wrote)
}
