package doctor

import (
	"context"
	"fmt"
	"sort"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func (d *Doctor) projectScopeCheck() Check {
	return Check{
		Name: "project-scope",
		Diagnose: func(ctx context.Context) Result {
			cwd, err := d.osProxy().Getwd()
			if err != nil {
				return Result{State: StateOK, Detail: "skipped: cannot resolve current directory: " + err.Error()}
			}

			markerPath, marker, err := configcommon.WalkUpFindMarker(cwd, d.osProxy())
			switch {
			case err != nil:
				return Result{
					State:  StateError,
					Detail: fmt.Sprintf("marker is malformed (%s); ignore it or fix the file. Falling back to machine-wide credential.", err),
				}
			case marker == nil:
				return Result{
					State:  StateOK,
					Detail: fmt.Sprintf("no %s found in %s or parents; machine-wide credential in use.", paths.ProjectMarkerFilename, cwd),
				}
			}

			slug := marker.Workspace
			matched := hasPerWorkspaceToken(ctx, d.resolver(), d.Envs, slug)

			if !matched {
				return Result{
					State: StateWarn,
					Detail: fmt.Sprintf(
						"marker at %s declares workspace %q%s, but no per-workspace token is stored; falling back to machine-wide credential. Run `bitrise-build-cache auth set --token <token> --workspace-id %s` to seed one.",
						markerPath, slug, toolsSuffix(marker), slug,
					),
				}
			}

			return Result{
				State:  StateOK,
				Detail: fmt.Sprintf("using per-workspace credential for %q (marker: %s%s)", slug, markerPath, toolsSuffix(marker)),
			}
		},
	}
}

func hasPerWorkspaceToken(ctx context.Context, r *live.Resolver, envs map[string]string, slug string) bool {
	_, _, matched, _ := r.ResolveNoRefreshForWorkspace(ctx, envs, slug) //nolint:dogsled // matched is the only signal we need

	return matched
}

func toolsSuffix(m *configcommon.ProjectMarker) string {
	if len(m.Tools) == 0 {
		return ""
	}

	var enabled, disabled []string
	for name, t := range m.Tools {
		switch {
		case t.Enabled == nil:
			continue
		case *t.Enabled:
			enabled = append(enabled, name)
		default:
			disabled = append(disabled, name)
		}
	}
	if len(enabled) == 0 && len(disabled) == 0 {
		return ""
	}
	sort.Strings(enabled)
	sort.Strings(disabled)

	parts := ""
	if len(enabled) > 0 {
		parts += fmt.Sprintf(" enabled=%v", enabled)
	}
	if len(disabled) > 0 {
		parts += fmt.Sprintf(" disabled=%v", disabled)
	}

	return "; tools" + parts
}
