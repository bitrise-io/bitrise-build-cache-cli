// Package live resolves the credential the CLI should use right now. It is the
// only place precedence and refresh exist; every consumer goes through it.
// See docs/auth.md.
package live

import (
	"context"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

// Prefer selects the precedence order.
type Prefer int

const (
	// PreferEnv is the default: an injected credential beats a stored one, so CI
	// and scripted runs use what they were handed.
	PreferEnv Prefer = iota
	// PreferStored puts the keychain and config file ahead of the env vars. Only
	// the interactive wizard uses it, so a stale token exported by a shell rc file
	// can't shadow a real login on the machine in front of the user.
	PreferStored
)

// OnRefreshFailure is what a caller wants when a store-managed credential cannot
// be refreshed.
type OnRefreshFailure int

const (
	// ServeStale is the default: hand back the stored credential. A slightly stale
	// token still authenticates far more often than it doesn't, and failing here
	// would take a whole build down over a transient network error.
	ServeStale OnRefreshFailure = iota
	// FailFast reports the failure instead. For interactive callers, where a dead
	// refresh token means "ask the user to sign in again" rather than "let the
	// backend reject it".
	FailFast
)

// Resolver answers "which credential should this process use". Nil fields take
// production defaults; Refresh and Store exist so tests stay off the real machine.
type Resolver struct {
	Logger log.Logger
	Prefer Prefer
	// OnRefreshFailure defaults to ServeStale.
	OnRefreshFailure OnRefreshFailure

	// Refresh renews a store-managed credential. Nil means the real OAuth flow.
	Refresh func(ctx context.Context, ts auth.TokenSet, backing store.Store) (auth.TokenSet, error)
	// Backends overrides the stores consulted, in order. Nil means keychain, file.
	Backends []store.Store
	// LegacyFile reads the multiplatform config's legacy authConfig key. Nil means
	// the real reader.
	LegacyFile func() (auth.Credential, bool)
}

// Resolve returns the credential to use, refreshing it first when it lives in a
// store this CLI manages. A refresh failure is not fatal: the stored credential is
// served as-is, because a slightly stale token still authenticates far more often
// than it doesn't, and failing here would take the whole build down.
func (r *Resolver) Resolve(ctx context.Context, envs map[string]string) (auth.Credential, auth.Origin, error) {
	cred, origin, backing, err := r.resolve(envs)
	if err != nil || !origin.StoreManaged() {
		return cred, origin, err
	}

	ts, refreshErr := r.refresh(ctx, backing)
	if refreshErr != nil {
		if r.OnRefreshFailure == FailFast {
			return cred, origin, refreshErr
		}
		r.debugf("serving the stored credential, refresh failed: %s", refreshErr)

		return cred, origin, nil
	}

	return ts.Credential(), ts.Origin(origin.Backend), nil
}

// ResolveNoRefresh is Resolve without the network or any write. `status` documents
// that it never refreshes, and the doctor must report what is on the machine rather
// than what a refresh would produce.
func (r *Resolver) ResolveNoRefresh(envs map[string]string) (auth.Credential, auth.Origin, error) {
	cred, origin, _, err := r.resolve(envs)

	return cred, origin, err
}

// Bind pins the environment for a long-lived process that resolves per RPC.
func (r *Resolver) Bind(envs map[string]string) *Bound {
	return &Bound{resolver: r, envs: envs}
}

// Bound is a Resolver with its environment fixed.
type Bound struct {
	resolver *Resolver
	envs     map[string]string
}

// Resolve returns the current credential and where it came from.
func (b *Bound) Resolve(ctx context.Context) (auth.Credential, auth.Origin, error) {
	return b.resolver.Resolve(ctx, b.envs)
}

// Get is the per-RPC credential accessor. It satisfies kv.AuthSource structurally,
// so live does not have to import the kv client. A resolution failure yields a zero
// credential: the RPC then fails on the server with a clear auth error, which is a
// better diagnostic than a nil-pointer panic inside the transport.
func (b *Bound) Get(ctx context.Context) auth.Credential {
	cred, _, err := b.resolver.Resolve(ctx, b.envs)
	if err != nil {
		b.resolver.debugf("no credential for this RPC: %s", err)
	}

	return cred
}

// Private — resolution

// resolve applies the precedence order and returns the backing store when the
// credential came from one, so the caller can refresh in place.
func (r *Resolver) resolve(envs map[string]string) (auth.Credential, auth.Origin, store.Store, error) {
	if r.Prefer == PreferStored {
		if cred, origin, backing, ok := r.fromStores(); ok {
			return cred, origin, backing, nil
		}
	}

	if hasAuthEnvVars(envs) {
		cred, origin, err := fromEnv(envs)

		return cred, origin, nil, err
	}

	if r.Prefer != PreferStored {
		if cred, origin, backing, ok := r.fromStores(); ok {
			return cred, origin, backing, nil
		}
	}

	if cred, ok := r.legacyFile(); ok {
		return cred, auth.Origin{Backend: auth.BackendFile, Provenance: auth.ProvenanceLegacy}, nil, nil
	}

	// Nothing stored: report the env vars as missing, which is the actionable error.
	cred, origin, err := fromEnv(envs)

	return cred, origin, nil, err
}

// fromStores walks the backends in order and returns the first populated record.
func (r *Resolver) fromStores() (auth.Credential, auth.Origin, store.Store, bool) {
	for _, s := range r.backends() {
		ts, err := s.Load()
		if err != nil || !ts.Populated() {
			continue
		}

		return ts.Credential(), ts.Origin(s.Backend()), s, true
	}

	return auth.Credential{}, auth.Origin{}, nil, false
}

func (r *Resolver) backends() []store.Store {
	if r.Backends != nil {
		return r.Backends
	}

	return []store.Store{store.NewKeychain(), store.NewFile()}
}

func (r *Resolver) legacyFile() (auth.Credential, bool) {
	if r.LegacyFile != nil {
		return r.LegacyFile()
	}

	return readLegacyFileCredential()
}

func (r *Resolver) refresh(ctx context.Context, backing store.Store) (auth.TokenSet, error) {
	ts, err := backing.Load()
	if err != nil {
		return auth.TokenSet{}, err //nolint:wrapcheck // store errors are already contextual
	}

	// A manual `auth set` token has no refresh token. Attempting the flow would
	// only produce ErrNotLoggedIn, which under FailFast would look like a dead
	// login rather than a perfectly good static credential.
	if !ts.IsOAuthManaged() {
		return ts, nil
	}

	if r.Refresh != nil {
		return r.Refresh(ctx, ts, backing)
	}

	cfg := oauth.NewConfigFromEnv(nil)
	cfg.Logger = r.Logger

	return cfg.EnsureFreshFrom(ctx, ts, backing) //nolint:wrapcheck // oauth wraps its own failures
}

func (r *Resolver) debugf(format string, args ...any) {
	if r.Logger != nil {
		r.Logger.Debugf(format, args...)
	}
}

func hasAuthEnvVars(envs map[string]string) bool {
	if envs[auth.EnvJWT] != "" {
		return true
	}

	return envs[auth.EnvAuthToken] != "" && envs[auth.EnvWorkspaceID] != ""
}

func fromEnv(envs map[string]string) (auth.Credential, auth.Origin, error) {
	token, workspaceID := envs[auth.EnvAuthToken], envs[auth.EnvWorkspaceID]

	if token != "" && workspaceID != "" {
		return auth.Credential{
				Token:       token,
				WorkspaceID: workspaceID,
				Username:    strings.TrimSpace(envs[auth.EnvUsername]),
			},
			auth.Origin{Backend: auth.BackendEnv, Provenance: auth.ProvenanceInjected},
			nil
	}

	// The JWT is always present on Bitrise CI and embeds the workspace.
	if jwt := envs[auth.EnvJWT]; jwt != "" {
		workspaceID, err := auth.ParseJWTWorkspaceID(jwt)
		if err != nil {
			return auth.Credential{}, auth.Origin{}, err //nolint:wrapcheck // already contextual
		}

		return auth.Credential{Token: jwt, WorkspaceID: workspaceID},
			auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceInjected},
			nil
	}

	if token == "" {
		return auth.Credential{}, auth.Origin{}, auth.ErrTokenNotProvided
	}

	return auth.Credential{}, auth.Origin{}, auth.ErrWorkspaceIDNotProvided
}

// Default is the production resolver. A nil logger is silent.
func Default(logger log.Logger) *Resolver {
	return &Resolver{Logger: logger}
}
