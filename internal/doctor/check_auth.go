package doctor

import (
	"context"
	"errors"
	"strings"

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
			// A workspace-less login is still unusable auth, so it fails the check —
			// but the fix is picking one, not signing in again.
			if errors.Is(err, auth.ErrWorkspaceNotSelected) {
				return Result{
					State:   StateError,
					Detail:  "signed in, but no workspace is selected",
					Fixable: true,
					Fixer:   WorkspacePickFixer{Prompt: d.WorkspacePickPrompt},
				}
			}
			resolved := err == nil && origin.Resolved()
			if res, ok := oidcResult(d.Envs, resolved); ok {
				return res
			}
			if !resolved {
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

// oidcResult covers a configured OIDC exchange, which this offline check never
// performs, so nothing on the machine yet is not "no credentials". A policy the job
// can't use is reported even when another credential resolved.
func oidcResult(envs map[string]string, resolved bool) (Result, bool) {
	if !auth.OIDCPolicyConfigured(envs) {
		return Result{}, false
	}

	var misconfigured error
	switch {
	case !auth.OnGitHubActionsOIDC(envs):
		misconfigured = auth.ErrNoGitHubOIDCToken
	case strings.TrimSpace(envs[auth.EnvWorkspaceID]) == "":
		misconfigured = auth.ErrOIDCWorkspaceIDMissing
	}

	switch {
	case misconfigured != nil && resolved:
		return Result{State: StateWarn, Detail: misconfigured.Error()}, true
	case misconfigured != nil:
		return Result{State: StateError, Detail: misconfigured.Error()}, true
	case !resolved:
		return Result{
			State:  StateOK,
			Detail: "GitHub Actions OIDC (trust policy " + strings.TrimSpace(envs[auth.EnvOIDCPolicyID]) + "), exchanged when a command first needs a credential",
		}, true
	}

	return Result{}, false
}

// storedFirstResolver reports what is stored on this machine. The `auth` check
// has always been keychain-first: it is a "what have you got here" diagnostic,
// and a stale shell-rc export should not be what it names.
func (d *Doctor) storedFirstResolver() *live.Resolver {
	r := d.resolver()
	r.Prefer = live.PreferStored

	return r
}

// resolver honours the doctor's injected backends and analytics-block reader so
// a diagnostic run in a test stays off the real machine. Default precedence, so
// the backend probe exercises the credential builds would actually send.
func (d *Doctor) resolver() *live.Resolver {
	r := live.Default(nil)
	if d.AuthBackends != nil {
		r.Backends = d.AuthBackends
		// Injected backends mean "stay off this machine", which has to cover the
		// analytics config too.
		r.AnalyticsBlock = func() (auth.Credential, auth.Origin, bool) {
			return auth.Credential{}, auth.Origin{}, false
		}
	}

	return r
}
