package doctor

import (
	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
)

func (d *Doctor) authCheck() Check {
	return Check{
		Name: "auth",
		Diagnose: func(_ context.Context) Result {
			// Read-only: report the credential that is on the machine, not the one a
			// refresh would produce.
			cred, origin, err := d.resolver().ResolveNoRefresh(d.Envs)
			if err != nil || !origin.Resolved() {
				return Result{
					State:   StateError,
					Detail:  "no credentials found",
					Fixable: true,
					Fixer:   AuthPromptFixer{Prompt: d.AuthFixPrompt},
				}
			}

			return Result{State: StateOK, Detail: live.Describe(cred, origin)}
		},
	}
}

// resolver honours the doctor's injected backends so a diagnostic run in a test
// stays off the real keychain.
func (d *Doctor) resolver() *live.Resolver {
	r := live.Default(nil)
	if d.AuthBackends != nil {
		r.Backends = d.AuthBackends
	}

	return r
}
