package doctor

import (
	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
)

func (d *Doctor) authCheck() Check {
	return Check{
		Name: "auth",
		Diagnose: func(_ context.Context) Result {
			// Read-only: report the credential that is on the machine, not the one a
			// refresh would produce.
			cred, origin, err := d.storedFirstResolver().ResolveNoRefresh(d.Envs)
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
