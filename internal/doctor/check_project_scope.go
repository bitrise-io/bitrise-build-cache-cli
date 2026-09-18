package doctor

import (
	"context"
	"fmt"
	"strings"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func (d *Doctor) projectScopeCheck() Check {
	return Check{
		Name: "project-scope",
		Diagnose: func(_ context.Context) Result {
			effective, sources, cfgErr := d.effectiveMachineConfig()
			if cfgErr != nil {
				return Result{
					State:  StateWarn,
					Detail: fmt.Sprintf("machine config unreadable: %s", cfgErr),
				}
			}

			mode := effective.ProjectMode

			cwd, err := d.osProxy().Getwd()
			if err != nil {
				return Result{State: StateOK, Detail: fmt.Sprintf("mode=%s (skipped marker walk: %s)", string(mode), err)}
			}

			markerPath, marker, err := machineconfig.WalkUpFindMarker(cwd, d.osProxy())
			if err != nil {
				return Result{
					State:  StateError,
					Detail: fmt.Sprintf("mode=%s; marker is malformed (%s); ignore it or fix the file.", string(mode), err),
				}
			}

			var lines []string
			lines = append(lines, fmt.Sprintf("mode=%s", string(mode)))

			push := *effective.CachePush
			lines = append(lines, fmt.Sprintf("cache_push=%t (%s)", push, sources.CachePush))

			if marker == nil {
				lines = append(lines, fmt.Sprintf("no %s found in %s or parents.", paths.ProjectMarkerFilename, cwd))
			} else {
				lines = append(lines, fmt.Sprintf("marker at %s", markerPath))
			}

			gates := mode == machineconfig.ModeOptIn && marker == nil
			lines = append(lines, fmt.Sprintf("would gate this directory: %s", yesNo(gates)))

			return Result{State: StateOK, Detail: strings.Join(lines, "; ")}
		},
	}
}

// effectiveMachineConfig resolves the machine config for doctor with an empty
// overlay (doctor never sees CLI flags). An unresolvable home dir yields
// defaults rather than an error — nothing could have been stored.
func (d *Doctor) effectiveMachineConfig() (machineconfig.Config, machineconfig.Sources, error) {
	current := machineconfig.Config{}
	if p, err := paths.Default(); err == nil {
		current, err = machineconfig.Read(d.osProxy(), p, nil)
		if err != nil {
			return machineconfig.Config{}, machineconfig.Sources{}, fmt.Errorf("read machine config: %w", err)
		}
	}

	effective, sources, err := machineconfig.Effective(machineconfig.FlagOverlay{}, current)
	if err != nil {
		return machineconfig.Config{}, machineconfig.Sources{}, fmt.Errorf("resolve machine config: %w", err)
	}

	return effective, sources, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}
