package doctor

import (
	"context"
	"fmt"
	"strings"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func (d *Doctor) projectScopeCheck() Check {
	return Check{
		Name: "project-scope",
		Diagnose: func(_ context.Context) Result {
			mode, cfgErr := d.effectiveProjectMode()
			if cfgErr != nil {
				return Result{
					State:  StateWarn,
					Detail: fmt.Sprintf("machine config unreadable: %s", cfgErr),
				}
			}

			cwd, err := d.osProxy().Getwd()
			if err != nil {
				return Result{State: StateOK, Detail: fmt.Sprintf("mode=%s (skipped marker walk: %s)", string(mode), err)}
			}

			markerPath, marker, err := configcommon.WalkUpFindMarker(cwd, d.osProxy())
			if err != nil {
				return Result{
					State:  StateError,
					Detail: fmt.Sprintf("mode=%s; marker is malformed (%s); ignore it or fix the file.", string(mode), err),
				}
			}

			var lines []string
			lines = append(lines, fmt.Sprintf("mode=%s", string(mode)))

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

// effectiveProjectMode returns the mode currently on disk, or the fallback when
// no preference was ever recorded. A read failure surfaces so the caller can
// distinguish a genuinely-empty config from an unreadable one.
func (d *Doctor) effectiveProjectMode() (machineconfig.Mode, error) {
	p, err := paths.Default()
	if err != nil {
		// A machine without a resolvable home dir can never have stored a
		// preference; treat that as the same "no override" case, not as an error.
		return machineconfig.ModeAlways, nil //nolint:nilerr // see comment
	}

	cfg, err := machineconfig.Read(d.osProxy(), p, nil)
	if err != nil {
		return "", fmt.Errorf("read machine config: %w", err)
	}
	if cfg.ProjectMode == "" {
		return machineconfig.ModeAlways, nil
	}

	return cfg.ProjectMode, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}
