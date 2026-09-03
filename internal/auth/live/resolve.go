// Package live resolves the credential the CLI should use right now. It is the
// only place precedence and refresh exist; every consumer goes through it.
// See docs/auth.md.
package live

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

type Prefer int

const (
	// PreferEnv is the default: an injected credential beats a stored one, so CI
	// and scripted runs use what they were handed.
	PreferEnv Prefer = iota
	// PreferStored puts the keychain and config file ahead of the env vars, so a
	// stale token exported by a shell rc file can't shadow a real login on the
	// machine in front of the user. The wizard and the doctor's auth check use it;
	// the doctor's backend probe deliberately does not, because it exists to test
	// the credential a build would actually send.
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
// production defaults; Refresh, Backends and AnalyticsBlock are the test seams.
type Resolver struct {
	Logger log.Logger
	Prefer Prefer
	// OnRefreshFailure defaults to ServeStale.
	OnRefreshFailure OnRefreshFailure

	// Refresh renews a store-managed credential. Nil means the real OAuth flow.
	Refresh func(ctx context.Context, ts auth.TokenSet, backing store.Store) (auth.TokenSet, error)
	// Backends overrides the stores consulted, in order. Nil means keychain, file.
	Backends []store.Store
	// AnalyticsBlock reads the analytics config's authConfig block. Nil means
	// the real reader.
	AnalyticsBlock func() (auth.Credential, auth.Origin, bool)
}

// Resolve returns the credential to use, refreshing it first when it lives in a
// store this CLI manages. A refresh failure is not fatal: the stored credential is
// served as-is, because a slightly stale token still authenticates far more often
// than it doesn't, and failing here would take the whole build down.
func (r *Resolver) Resolve(ctx context.Context, envs map[string]string) (auth.Credential, auth.Origin, error) {
	return r.resolveAndRefresh(ctx, envs, auth.TokenSet.Populated)
}

// ResolveForWorkspace is Resolve with a workspace hint. An empty slug is
// identical to Resolve. A matching per-workspace entry in any store backend
// wins over whatever Resolve returned — an env-var or static-file credential
// is machine-wide, and the caller asked for the workspace-scoped one. An
// unknown slug warns and falls back so a build is never blocked.
func (r *Resolver) ResolveForWorkspace(ctx context.Context, envs map[string]string, workspaceID string) (auth.Credential, auth.Origin, error) {
	cred, origin, err := r.Resolve(ctx, envs)
	if err != nil || workspaceID == "" {
		return cred, origin, err
	}

	ws, ok, backend := r.lookupWorkspace(workspaceID)
	if !ok {
		if r.Logger != nil {
			r.Logger.Warnf("no per-workspace credential for %q; falling back to the machine-wide credential", workspaceID)
		}

		return cred, origin, nil
	}

	return ws.Credential(), ws.Origin(backend), nil
}

func (r *Resolver) lookupWorkspace(slug string) (auth.TokenSet, bool, auth.Backend) {
	for _, s := range r.backends() {
		ts, err := s.Load()
		if err != nil {
			continue
		}
		if entry, ok := ts.ForWorkspace(slug); ok {
			return entry, true, s.Backend()
		}
	}

	return auth.TokenSet{}, false, auth.BackendNone
}

func (r *Resolver) resolveAndRefresh(ctx context.Context, envs map[string]string, usable func(auth.TokenSet) bool) (auth.Credential, auth.Origin, error) {
	cred, origin, backing, err := r.resolveWith(envs, usable)
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

// ResolveNoRefreshForWorkspace is ResolveNoRefresh with a workspace hint. The
// matched bool is true when a per-workspace record was found; an empty or
// unknown slug silently falls back to the machine-wide credential with
// matched=false, so callers on the wrapper hot path never block a build on a
// bad marker.
func (r *Resolver) ResolveNoRefreshForWorkspace(_ context.Context, envs map[string]string, workspaceID string) (auth.Credential, auth.Origin, bool, error) {
	cred, origin, err := r.ResolveNoRefresh(envs)
	if err != nil || workspaceID == "" {
		return cred, origin, false, err
	}

	ws, ok, backend := r.lookupWorkspace(workspaceID)
	if !ok {
		return cred, origin, false, nil
	}

	return ws.Credential(), ws.Origin(backend), true, nil
}

// ResolveTokenOnly is Resolve for the one caller that needs a token before a
// workspace exists: the `auth workspace` listing. The Credential it returns may
// carry an empty WorkspaceID, so nothing that talks to the cache may use it.
func (r *Resolver) ResolveTokenOnly(ctx context.Context, envs map[string]string) (auth.Credential, auth.Origin, error) {
	return r.resolveAndRefresh(ctx, envs, hasToken)
}

// For a long-lived process that resolves per RPC.
func (r *Resolver) Bind(envs map[string]string) *Bound {
	return &Bound{resolver: r, envs: envs}
}

type Bound struct {
	resolver *Resolver
	envs     map[string]string
}

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

func (r *Resolver) resolve(envs map[string]string) (auth.Credential, auth.Origin, store.Store, error) {
	return r.resolveWith(envs, auth.TokenSet.Populated)
}

// resolveWith applies the precedence order and returns the backing store when the
// credential came from one, so the caller can refresh in place. usable decides
// which stored records count.
func (r *Resolver) resolveWith(envs map[string]string, usable func(auth.TokenSet) bool) (auth.Credential, auth.Origin, store.Store, error) {
	if r.Prefer == PreferStored {
		if cred, origin, backing, ok := r.fromStores(usable); ok {
			return cred, origin, backing, nil
		}
	}

	if hasAuthEnvVars(envs) {
		cred, origin, err := fromEnv(envs)

		return cred, origin, nil, err
	}

	if r.Prefer != PreferStored {
		if cred, origin, backing, ok := r.fromStores(usable); ok {
			return cred, origin, backing, nil
		}
	}

	if cred, origin, ok := r.legacyFile(); ok {
		return cred, origin, nil, nil
	}

	// A login that stopped before the workspace step is not "no credentials" —
	// saying so would send the user off to create a token they already have.
	if r.hasTokenWithoutWorkspace() {
		return auth.Credential{}, auth.Origin{}, nil, auth.ErrWorkspaceNotSelected
	}

	// Nothing stored: report the env vars as missing, which is the actionable error.
	cred, origin, err := fromEnv(envs)

	return cred, origin, nil, err
}

func (r *Resolver) hasTokenWithoutWorkspace() bool {
	for _, s := range r.backends() {
		if ts, err := s.Load(); err == nil && ts.AuthToken != "" && ts.WorkspaceID == "" {
			return true
		}
	}

	return false
}

func hasToken(ts auth.TokenSet) bool { return ts.AuthToken != "" }

// fromStores prefers an OAuth-managed record wherever it lives: a manual token in
// an earlier backend would otherwise hide a login in a later one, so the login
// would never be refreshed. Falls back to the first usable record.
func (r *Resolver) fromStores(usable func(auth.TokenSet) bool) (auth.Credential, auth.Origin, store.Store, bool) {
	var (
		firstTS    auth.TokenSet
		firstStore store.Store
	)

	for _, s := range r.backends() {
		ts, err := s.Load()
		if err != nil || !usable(ts) {
			continue
		}
		if ts.IsOAuthManaged() {
			return ts.Credential(), ts.Origin(s.Backend()), s, true
		}
		if firstStore == nil {
			firstTS, firstStore = ts, s
		}
	}

	if firstStore == nil {
		return auth.Credential{}, auth.Origin{}, nil, false
	}

	return firstTS.Credential(), firstTS.Origin(firstStore.Backend()), firstStore, true
}

func (r *Resolver) backends() []store.Store {
	if r.Backends != nil {
		return r.Backends
	}

	return []store.Store{store.NewKeychain(), store.NewFile()}
}

func (r *Resolver) legacyFile() (auth.Credential, auth.Origin, bool) {
	if r.AnalyticsBlock != nil {
		return r.AnalyticsBlock()
	}

	return readAnalyticsCredential()
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
		return auth.Credential{Token: token, WorkspaceID: workspaceID},
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

// A nil logger is silent.
func Default(logger log.Logger) *Resolver {
	return &Resolver{Logger: logger}
}
