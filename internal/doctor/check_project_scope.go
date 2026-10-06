package doctor

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

const (
	pushSourceStored  = "machine config"
	pushSourceDefault = "default"
)

func (d *Doctor) projectScopeCheck() Check {
	return Check{
		Name: "project-scope",
		Diagnose: func(_ context.Context) Result {
			current, cfgErr := d.readMachineConfig()
			if cfgErr != nil {
				return Result{
					State:  StateWarn,
					Detail: fmt.Sprintf("machine config unreadable: %s", cfgErr),
				}
			}

			mode := machineconfig.ResolvedProjectMode(current)

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

			lines := []string{
				fmt.Sprintf("Detected project: %s", cwd),
			}

			if marker == nil {
				lines = append(lines, fmt.Sprintf("Opt-in marker: none (searched from %s)", cwd))
			} else {
				markerLine := fmt.Sprintf("Opt-in marker: %s", markerPath)
				if filepath.Dir(markerPath) != cwd {
					markerLine += " (inherited from ancestor)"
				}
				lines = append(lines, markerLine)
			}

			lines = append(lines, fmt.Sprintf("Mode: %s", string(mode)))

			push := machineconfig.ResolvedCachePush(current)
			pushSource := pushSourceDefault
			if current.CachePush != nil {
				pushSource = pushSourceStored
			}
			lines = append(lines, fmt.Sprintf("Cache push: %s (%s)", enabledDisabled(push), pushSource))

			active := mode != machineconfig.ModeOptIn || marker != nil
			lines = append(lines, fmt.Sprintf("Cache active here: %s", yesNo(active)))

			return Result{State: StateOK, Detail: strings.Join(lines, "\n  ")}
		},
	}
}

func enabledDisabled(b bool) string {
	if b {
		return "enabled"
	}

	return "disabled"
}

// readMachineConfig returns the persisted config. An unresolvable home dir
// yields an empty config with no error — nothing could have been stored.
func (d *Doctor) readMachineConfig() (machineconfig.Config, error) {
	p, pathErr := paths.Default()
	if pathErr != nil {
		return machineconfig.Config{}, nil //nolint:nilerr // unresolvable home = no stored config, not an error
	}

	cfg, err := machineconfig.Read(d.osProxy(), p, nil)
	if err != nil {
		return machineconfig.Config{}, fmt.Errorf("read machine config: %w", err)
	}

	return cfg, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}
