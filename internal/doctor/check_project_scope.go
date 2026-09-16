package doctor

import (
	"context"
	"fmt"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func (d *Doctor) projectScopeCheck() Check {
	return Check{
		Name: "project-scope",
		Diagnose: func(_ context.Context) Result {
			cwd, err := d.osProxy().Getwd()
			if err != nil {
				return Result{State: StateOK, Detail: "skipped: cannot resolve current directory: " + err.Error()}
			}

			markerPath, marker, err := configcommon.WalkUpFindMarker(cwd, d.osProxy())
			switch {
			case err != nil:
				return Result{
					State:  StateError,
					Detail: fmt.Sprintf("marker is malformed (%s); ignore it or fix the file.", err),
				}
			case marker == nil:
				return Result{
					State:  StateOK,
					Detail: fmt.Sprintf("no %s found in %s or parents.", paths.ProjectMarkerFilename, cwd),
				}
			}

			return Result{
				State:  StateOK,
				Detail: fmt.Sprintf("marker at %s", markerPath),
			}
		},
	}
}
