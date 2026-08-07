package live

import (
	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

// ResolvePinned resolves, and writes an env- or JWT-sourced credential to disk so
// the processes activation starts — the xcelerate proxy, the ccache storage helper,
// the Gradle init script — can find it in a shell that never saw those env vars.
//
// The write is read-modify-write against the existing record, so it cannot drop a
// refresh token, a JWT or a display name. A write failure is logged, not returned:
// the credential resolved fine and the activation should proceed.
func (r *Resolver) ResolvePinned(ctx context.Context, envs map[string]string, isCI bool) (auth.Credential, auth.Origin, error) {
	cred, origin, err := r.Resolve(ctx, envs)
	if err != nil || origin.StoreManaged() {
		return cred, origin, err
	}

	// A CI JWT is short-lived and workspace-embedded; it goes to the legacy
	// authConfig key that the analytics and React Native readers expect, and never
	// into the credentials block, which is for credentials that outlive one build.
	if origin.Backend == auth.BackendJWT {
		if legacyErr := writeLegacyFileCredential(cred); legacyErr != nil {
			r.debugf("could not mirror the CI JWT to the analytics config: %s", legacyErr)
		}

		return cred, origin, nil
	}

	target := r.pinTarget(isCI)

	existing, loadErr := target.Load()
	if loadErr != nil {
		existing = auth.TokenSet{}
	}
	existing.AuthToken, existing.WorkspaceID = cred.Token, cred.WorkspaceID
	if cred.Username != "" {
		existing.Username = cred.Username
	}

	result, saveErr := store.SaveWithFallback(target, existing, true)
	if saveErr != nil {
		r.debugf("could not persist the resolved credential: %s", saveErr)

		return cred, origin, nil
	}
	result.WarnFallback(r.Logger)

	return cred, origin, nil
}

func (r *Resolver) pinTarget(isCI bool) store.Store {
	if len(r.Backends) > 0 {
		return r.Backends[0]
	}

	return store.SelectAuto(isCI)
}
