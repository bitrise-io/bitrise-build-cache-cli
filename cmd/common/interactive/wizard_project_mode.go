package interactive

import (
	"charm.land/huh/v2"
	"github.com/bitrise-io/go-utils/v2/log"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func projectModePrompt(logger log.Logger) (machineconfig.Mode, error) {
	osProxy := utils.DefaultOsProxy{}

	p, err := paths.Default()
	if err != nil {
		return machineconfig.ModeAlways, err //nolint:wrapcheck // caller wraps
	}

	current, err := machineconfig.Read(osProxy, p, logger)
	if err != nil {
		logger.Warnf("Could not read the machine-wide build-cache config (%v); starting from 'always'.", err)
		current = machineconfig.Config{ProjectMode: machineconfig.ModeAlways}
	}
	if current.ProjectMode == "" {
		current.ProjectMode = machineconfig.ModeAlways
	}

	choice := string(current.ProjectMode)
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

	return mode, nil
}
