package doctor

import (
	"context"
	"errors"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
)

func (d *Doctor) authCheck() Check {
	return Check{
		Name: "auth",
		Diagnose: func(_ context.Context) Result {
			// Read-only: report the credential that is on the machine, not the one a
			// refresh would produce.
			cred, origin, workspacesOnly, err := d.storedFirstResolver().ResolveNoRefresh(d.Envs)
			scenario := d.authScenario(workspacesOnly, origin, err)
			if workspacesOnly {
				return Result{State: StateOK, Detail: "scenario B (per-workspace only): resolved per build via project marker"}
			}
			// A workspace-less login is still unusable auth, so it fails the check —
			// but the fix is picking one, not signing in again.
			if errors.Is(err, auth.ErrWorkspaceNotSelected) {
				return Result{
					State:   StateError,
					Detail:  "signed in, but no workspace is selected (scenario " + scenario + ")",
					Fixable: true,
					Fixer:   WorkspacePickFixer{Prompt: d.WorkspacePickPrompt},
				}
			}
			if err != nil || !origin.Resolved() {
				return Result{
					State:   StateError,
					Detail:  "no credentials found (scenario " + scenario + ")",
					Fixable: true,
					Fixer:   AuthPromptFixer{Prompt: d.AuthFixPrompt},
				}
			}

			return Result{State: StateOK, Detail: "scenario " + scenario + ": " + live.Describe(cred, origin)}
		},
	}
}

// authScenario names the store layout: A (machine-wide only), B (per-workspace
// only), C (both — machine-wide as fallback), None.
func (d *Doctor) authScenario(workspacesOnly bool, origin auth.Origin, resolveErr error) string {
	if workspacesOnly {
		return "B"
	}

	hasPerWorkspace := d.storedFirstResolver().StoreHasAnyWorkspaces()

	machineWide := origin.Resolved() && resolveErr == nil
	switch {
	case machineWide && hasPerWorkspace:
		return "C"
	case machineWide:
		return "A"
	}

	return "None"
}

// storedFirstResolver reports what is stored on this machine. The `auth` check
// has always been keychain-first: it is a "what have you got here" diagnostic,
// and a stale shell-rc export should not be what it names.
func (d *Doctor) storedFirstResolver() *live.Resolver {
	r := d.resolver()
	r.Prefer = live.PreferStored

	return r
}

// resolver honours the doctor's injected backends and legacy reader so a
// diagnostic run in a test stays off the real machine. Default precedence, so
// the backend probe exercises the credential builds would actually send.
func (d *Doctor) resolver() *live.Resolver {
	r := live.Default(nil)
	if d.AuthBackends != nil {
		r.Backends = d.AuthBackends
		// Injected backends mean "stay off this machine", which has to cover the
		// legacy analytics config too.
		r.AnalyticsBlock = func() (auth.Credential, auth.Origin, bool) {
			return auth.Credential{}, auth.Origin{}, false
		}
	}

	return r
}
