# Authentication architecture

Every credential the CLI uses — a CI JWT, an env-var PAT, a browser login in the OS
keychain, a CI-safe token in the config file — resolves through one function. This
document is the map: what each package owns, what may import what, and how a
credential travels from storage to an authenticated RPC.

## Layers

Arrows point down. No package may import at or above its own level.

```
L5  CONSUMERS
    cmd/{auth,gradle,xcode,common,common/interactive}
    pkg/{file,browse,ccache,reactnative}
    internal/config/{gradle,bazel,ccache,xcelerate}
    internal/{doctor,bazelcredhelper,build_cache/kv,xcelerate/proxy}
         │  imports: live, auth · (store/oauth only for login, logout, clear)
         ▼
L4  internal/auth/live                  the resolver
         │  imports: auth, store, oauth
         ▼
L3  internal/auth/oauth                 sign-in · refresh · token exchange
         │  imports: auth, store
         ▼
L2  internal/auth/store                 backend selection · persistence
         │  imports: auth, keychain, config/multiplatform
         ├────────────────────────┐
         ▼                        ▼
L1  internal/auth/keychain   internal/config/multiplatform
         │  imports: auth          │  imports: auth
         └───────────┬─────────────┘
                     ▼
L0  internal/auth                       Credential · TokenSet · Origin
                     imports: nothing internal
```

`internal/config/common` is **not** in this graph. It holds cache config, endpoint
selection, build metadata, benchmark phasing and `DetectCIProvider` — consumer-level
utilities that sit beside L5. No auth package imports it.

### Invariants

| # | Invariant | Why |
|---|---|---|
| 1 | `internal/auth` (L0) imports no internal package | It is the shared vocabulary. A dependency here forces every type that wants to be on the boundary to route around a cycle. |
| 2 | `auth.TokenSet` never appears above L4 | Consumers that don't need a refresh token must not be handed one. |
| 3 | Consumers resolve **only** through `live` | One precedence order, one refresh implementation. |
| 4 | No auth package imports `internal/config/common` | Keeps the auth graph a strict tree; `config/common` is a consumer-level utility, not a layer. |

Enforced by `make lint-arch`, not by convention. See [Enforcement](#enforcement).

## The two credential types

There are exactly two, and the boundary between them is *what you authenticate with*
versus *what you persist*.

```
        L1–L3 storage                            L4 boundary
   ┌──────────────────────┐              ┌──────────────────────┐
   │   auth.TokenSet      │              │   auth.Credential    │
   │   AuthToken          │ ───────────► │   Token              │
   │   WorkspaceID        │ .Credential()│   WorkspaceID        │
   │   Username           │              │   Username           │
   │   PATExpiry          │              │   Expiry             │
   │   JWT                │              └──────────────────────┘
   │   JWTExpiry          │      ✗
   │   RefreshToken       │ ◄──── never ────
   │   RefreshTokenExpiry │
   └──────────────────────┘
              │
              └─ .Origin(Backend) → Origin{Backend, Provenance}
                    Provenance derived from IsOAuthManaged()
```

**`TokenSet` is the persisted record.** One type for both backends — the OS keychain
and the CI-safe config file store the same JSON.

**`Credential` is what a caller needs to make one authenticated call.** Four fields,
all always meaningful. Twenty-plus consumers take this; none of them needs the
refresh machinery, and handing it over is how a refresh token ends up in a log line
or a rendered config template.

**The conversion is one-directional and there is no reverse.** `Credential` cannot be
turned back into a `TokenSet`. Writes load the existing record, mutate the fields
they own, and save it back — so a write path can never drop a refresh token, a JWT or
a display name it wasn't thinking about.

### Origin

`Origin` splits two independent axes that a single enum conflates:

```go
type Origin struct {
    Backend    Backend     // None | Env | JWT | Keychain | File
    Provenance Provenance  // None | Injected | OAuthLogin | Manual | Legacy
}
```

The config file holds credentials under two JSON keys: `credentials`, written by
`auth login` and `auth set`, and the legacy `authConfig`, read by older analytics
consumers. Both are `Backend == File`; they differ in `Provenance`
(`OAuthLogin`/`Manual` vs `Legacy`). Backend alone cannot express that, and
provenance alone cannot tell you which file to write.

`Origin.StoreManaged()` is `Backend ∈ {Keychain, File}` minus the legacy block —
the predicate that gates refresh. Both backends can hold an OAuth login, so both
are refreshable; test the predicate, never a specific backend. A record with no
refresh token is skipped inside `Resolve` regardless: attempting the flow could
only produce `ErrNotLoggedIn`, which a `FailFast` caller would misread as a dead
login rather than a perfectly good static credential.

## Package reference

### `internal/auth` (L0)

The shared vocabulary. Imports nothing internal.

| Export | Purpose |
|---|---|
| `Credential{Token, WorkspaceID, Username, Expiry}` | The boundary type. What you authenticate with. |
| `Credential.Expired() bool` | Known expiry is in the past. |
| `TokenSet{AuthToken, WorkspaceID, Username, PATExpiry, JWT, JWTExpiry, RefreshToken, RefreshTokenExpiry}` | The persisted record, identical for both backends. |
| `TokenSet.Credential() Credential` | The only conversion. Narrows the record to the boundary. |
| `TokenSet.Origin(Backend) Origin` | Pairs a backend with the provenance implied by the record. |
| `TokenSet.IsOAuthManaged() bool` | Has a refresh token, so it came from `auth login`. |
| `Backend`, `Provenance`, `Origin` | Where a credential lives, and how it got there. |
| `Origin.StoreManaged() bool` | Gates refresh. |
| `Origin.Label() string` | Prose name — `"OAuth login (config file)"`. |
| `Origin.ShortLabel() string` | Compact name — `"config-file"`. For diagnostics and `--json`. |
| `GradleToken(Credential, Origin) string` | JWT is sent as-is; a PAT is `workspaceID:token`. Needs both, hence a free function. |
| `ParseJWTWorkspaceID(string) (string, error)` | Extracts `org_id` from the Bitrise UMA JWT. Signature unverified — Bitrise mints these per build. |
| `EnvAuthToken`, `EnvWorkspaceID`, `EnvJWT`, `EnvUsername` | The four environment keys. |
| `ErrTokenNotProvided`, `ErrWorkspaceIDNotProvided` | Distinguish "nothing configured" from a genuine failure. |

### `internal/auth/keychain` (L1)

OS keyring I/O and nothing else.

| Export | Purpose |
|---|---|
| `New() *Keychain` | Handle on the platform keyring. |
| `(*Keychain).Load/Save/Clear` | The three operations, on `auth.TokenSet`. |
| `Backend` interface, `NewBackend()` | Injection seam for the doctor's keyring probe. |
| `ErrNotFound` | Nothing stored. |
| `ErrUnavailable` | No usable keyring on this host — headless Linux, containers. Different user advice from `ErrNotFound`. |
| `Unavailable(err) bool` | Classifies a backend error as "no keyring". |

### `internal/config/multiplatform` (L1)

The CI-safe file backend. Owns the on-disk shape; imports `auth` for `TokenSet`.

| Export | Purpose |
|---|---|
| `Config{Credentials *auth.TokenSet, AuthConfig LegacyAuthConfig, DebugLogging}` | The file. |
| `LegacyAuthConfig{AuthToken, WorkspaceID, IsJWT}` | The pre-`credentials` block, still read by analytics consumers and older CLI versions. Its **field names are the wire format** — they must not be renamed to match `Credential`, which is why this is a separate type rather than a reuse. |
| `ReadCredentials`, `SaveCredentials`, `ClearCredentials` | Credential access. `SaveCredentials` is read-modify-write and mirrors into `AuthConfig`. |
| `Update(osProxy, enc, dec, mutate func(*Config)) error` | Read-modify-write for non-credential fields. |
| `ReadConfig`, `FilePath` | Whole-file access. |

`Config.Save` is a full overwrite. Never call it on a freshly constructed `Config`
that you intend to merge into an existing file — it will drop the `credentials`
block, refresh token included, on hosts where the file is the only backend. Use
`Update`.

### `internal/auth/store` (L2)

Which backend a credential lives in, and getting it in and out.

| Export | Purpose |
|---|---|
| `Store` interface `{Backend() auth.Backend; Load; Save; Clear}` | The backend contract. |
| `NewKeychain()`, `NewFile()` | The two backends. |
| `SelectAuto(isCI bool) Store` | CI → file, local → keychain. On CI, `fastlane setup_ci` swaps the keychain out from under us. |
| `Select(isCI bool, override string) (Store, error)` | Honours `--storage=keychain\|file\|auto`. |
| `SaveExclusiveWithFallback(target, ts, allowFallback) (SaveResult, error)` | Saves and clears every other backend, dropping to the file store when the keychain refuses so a keyring-less host can still hold a login. Exclusive because it backs a deliberate user action (`auth login`, `auth set`) where two populated backends would be split-brain. `allowFallback` is false when the caller named a backend explicitly. |
| `SaveWithFallback(target, ts, allowFallback) (SaveResult, error)` | Same, but leaves the other backends alone. Activation uses this: clearing the other backend there would throw away a login the user deliberately stored. |
| `SaveResult{Origin, KeychainErr}` | Where it landed, and why it fell back. A non-nil `KeychainErr` *is* the fallback signal. |
| `SaveResult.WarnFallback(log.Logger)` | The one place the fallback warning is written. |
| `SetUsername(isCI bool, name) (auth.Origin, error)` | Writes a display name into whichever backend already holds credentials, so it can't strand an empty-token record in the other one. |
| `ErrNotFound` | Backend-agnostic "nothing stored". Wraps `keychain.ErrUnavailable` rather than flattening it, so `errors.Is` answers both questions and no caller needs to bypass the interface. |

`store` takes `isCI bool`, never an env map. CI-ness affects only the *write* target;
read precedence is identical everywhere. The caller runs
`configcommon.DetectCIProvider(envs) != ""` and passes the answer.

### `internal/auth/oauth` (L3)

The browser sign-in and the refresh primitive.

| Export | Purpose |
|---|---|
| `Config`, `NewConfigFromEnv(envs)` | Issuer, client ID, endpoints; env-overridable. |
| `(Config).Login(ctx, openBrowser) (auth.TokenSet, error)` | Full PKCE flow: authorize → code → JWT → PAT. |
| `(Config).EnsureFreshFrom(ctx, ts, backing store.Store) (auth.TokenSet, error)` | Refresh-if-needed under a cross-process lock, saving back to the store the record came from. Takes the already-loaded record so it cannot disagree with the caller's view, and so the store is read once per resolve. |
| `LoadWithSource() (auth.TokenSet, store.Store, error)` | "Which backend holds the login" — a storage question, preferring an OAuth-managed record over a manual one wherever it lives. Not a resolve. |
| `SaveToWithFallback(s, ts, allowFallback) (store.SaveResult, error)` | Persists a completed sign-in, refresh token included. |
| `ClearFrom(backends...)` | Clears only the named backend, so logout can't take an `auth set` token with it. |
| `OpenBrowser`, `CallbackFallback`, `ParsePastedCallback` | Browser launch, and the paste-the-URL fallback for headless or SSH sessions. |
| `ErrNotLoggedIn`, `ErrLoginRequired` | Distinguish "never signed in" from "session expired". Drive the Bazel helper's warning text. |
| `RefreshSkew` | How far ahead of expiry a refresh triggers. The Bazel helper's `expiresLead` must stay below it. |

`EnsureFreshFrom` is the single refresh primitive and is called from exactly one
place: `live.Resolver.Resolve`.

### `internal/auth/live` (L4)

The resolver. The only package a consumer needs.

| Export | Purpose |
|---|---|
| `Resolver{Logger, Prefer, Refresh, Store}` | The facade. Nil fields take production defaults; `Refresh` and `Store` are the test seams. |
| `Prefer` (`PreferEnv` default, `PreferStored`) | `PreferStored` puts the store ahead of env vars. The interactive wizard uses it so a stale `BITRISE_BUILD_CACHE_AUTH_TOKEN` in a shell rc file can't shadow a real login. Nothing else should. |
| `(*Resolver).Resolve(ctx, envs) (Credential, Origin, error)` | **The one resolve path.** Precedence, then refresh when store-managed. |
| `Resolver.OnRefreshFailure` (`ServeStale` default, `FailFast`) | What to do when a store-managed credential cannot be refreshed. `ServeStale` hands back the stored token — a slightly stale token still authenticates far more often than it doesn't, and failing would take a build down over a transient error. `FailFast` reports it: the wizard and the Bazel helper both need to act on a dead refresh token rather than serve one the backend will reject. |
| `(*Resolver).ResolveNoRefresh(envs) (Credential, Origin, error)` | Same precedence, no network, no writes. For `status`, which documents that it never refreshes. |
| `(*Resolver).ResolvePinned(ctx, envs, isCI) (Credential, Origin, error)` | Resolve, and materialise an ephemeral env- or JWT-sourced credential to disk so processes started by `activate` can find it without the env vars. Read-modify-write, and **not** exclusive — see `store.SaveWithFallback`. Returns the origin the credential *resolved* from, not where the copy landed. |
| `(*Resolver).Bind(envs) *Bound` | Pins the environment for a long-lived process. |
| `(*Bound).Get(ctx) auth.Credential` | Per-RPC credential. Structurally satisfies `kv.AuthSource` without `live` importing `kv`. |
| `Describe(Credential, Origin) string` | The one-line human description. Pure formatting, no I/O — `Origin` already carries everything it needs. |

**Precedence**, in `Resolve`, `ResolveNoRefresh` and `ResolvePinned` alike:

```
env vars (AUTH_TOKEN + WORKSPACE_ID)
  → CI JWT (BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN)
  → OS keychain
  → config file, `credentials` key
  → config file, legacy `authConfig` key
```

`PreferStored` moves the two file/keychain steps ahead of the env vars. That is the
only variation, and it exists for one caller.

## Call chains

### CI build — JWT, no disk, no network

```
cmd/gradle.enableForGradle
└─ live.Resolver.Resolve(ctx, envs)                                  L4
   └─ resolve(envs)
      ├─ envs[EnvAuthToken] + [EnvWorkspaceID]        miss
      └─ envs[EnvJWT]                                 hit
         └─ auth.ParseJWTWorkspaceID(jwt)                            L0
      ⇒ Credential{Token: jwt, WorkspaceID: org}, Origin{JWT, Injected}
   └─ origin.StoreManaged() == false → return
└─ auth.GradleToken(cred, origin) → jwt as-is                        L0
```

### Local dev — stored login, PAT inside the refresh window

```
cmd/auth.authTokenCmd
└─ live.Resolver.Resolve(ctx, envs)                                  L4
   ├─ resolve(envs)
   │  ├─ env / JWT                                    miss
   │  └─ store.NewKeychain().Load()                                  L2
   │     └─ keychain.Keychain.Load()                                 L1
   │        ⇒ auth.TokenSet                                          L0
   │     ⇒ ts.Credential(), ts.Origin(BackendKeychain)
   │        └─ ts.IsOAuthManaged() → Provenance = OAuthLogin
   ├─ origin.StoreManaged() == true
   └─ oauth.Config.EnsureFreshFrom(ctx, ts, backingStore)            L3
      ├─ now+RefreshSkew < ts.PATExpiry → return unchanged
      ├─ refreshlock.Acquire()                        cross-process
      ├─ record not OAuth-managed → return unchanged, no refresh attempted
      ├─ POST /oauth2/token   refresh → JWT
      ├─ POST /oidc/token     JWT → PAT
      └─ oauth.SaveToWithFallback(backingStore, ts', false)          L3
         └─ store.SaveWithFallback(target, ts', false)               L2
            └─ Store.Save(ts')
      ⇒ ts'
   ⇒ ts'.Credential()          Expiry = PATExpiry
└─ auth.GradleToken(cred, origin) → "workspaceID:pat"
```

### `activate` — the pin path

```
config/xcelerate.Activate(ctx, …, envs)
├─ isCI := configcommon.DetectCIProvider(envs) != ""     caller decides
├─ live.Resolver.ResolvePinned(ctx, envs, isCI)                      L4
│  ├─ resolve(envs) ⇒ Credential, Origin{Env, Injected}
│  ├─ Backend ∈ {Env, JWT} → ephemeral, must be materialised
│  ├─ store.SelectAuto(isCI) → fileStore on CI                       L2
│  ├─ existing, _ := target.Load()                      read-modify-write
│  ├─ existing.AuthToken, .WorkspaceID = cred.Token, cred.WorkspaceID
│  │     RefreshToken, Username, JWT preserved — no reverse conversion exists
│  └─ store.SaveWithFallback(target, existing, true)    non-exclusive
│  ⇒ Credential, Origin{Env, Injected}   ← where it resolved from
├─ NewConfig(ctx, logger, params, cred, envs, …)        takes the credential
├─ config.Save(...)
└─ multiplatformconfig.Update(…, func(c *Config) { c.DebugLogging = … })
```

Two independent read-modify-writes, each owning its own fields. The activation path
must never construct a fresh `multiplatformconfig.Config` and `Save` it.

### `auth login`

```
cmd/common/interactive.runLogin
├─ cfg := oauth.NewConfigFromEnv(envs)                               L3
├─ cfg.Login(ctx, oauth.OpenBrowser)
│  ├─ PKCE + state
│  ├─ callback server  ⟂  cfg.CallbackFallback      paste-the-URL
│  ├─ POST /oauth2/token   code → JWT
│  └─ POST /oidc/token     JWT → PAT
│  ⇒ auth.TokenSet                                                   L0
├─ pickWorkspace(ctx, envs, ts.AuthToken) → ts.WorkspaceID
├─ store.Select(isCI, loginStorage)                                  L2
└─ oauth.SaveToWithFallback(target, ts, loginStorage == "")          L3
   └─ store.SaveWithFallback(target, ts, allowFallback)              L2
      ├─ ok       ⇒ SaveResult{Origin{Keychain, OAuthLogin}}
      └─ refused  ⇒ NewFile().Save(ts)
                  ⇒ SaveResult{Origin{File, OAuthLogin}, KeychainErr}
├─ res.WarnFallback(logger)
└─ logger.Infof("Signed in. … %s", res.Origin.Label())
```

### `auth logout`

```
cmd/common/interactive.LogoutCmd
├─ oauth.LoadWithSource()                                            L3
│  └─ loadFrom(store.NewKeychain(), store.NewFile())                 L2
│     prefers the OAuth-managed record wherever it lives
├─ ts.IsOAuthManaged() == false → nothing to remove
└─ oauth.ClearFrom(source)      only the holding backend             L3
```

Logout does not go through `live`: "which backend holds the login" is a storage
question, not a resolution.

### Long-lived processes — per-RPC resolution

```
cmd/xcode.startProxy
└─ bound := (&live.Resolver{Logger: l}).Bind(envs)                   L4

  ── per RPC ──
  kv.Client.Do(ctx, …)
  └─ authSource.Get(ctx)          satisfied by *live.Bound
     └─ live.Resolver.Resolve(ctx, envs)
     ⇒ auth.Credential{… Expiry: real PATExpiry}
```

The xcelerate proxy, the ccache IPC storage helper and the Bazel credential helper
all use this shape. `ctx` arrives per call; none of them holds one in a struct.

The Bazel helper wraps it with failure policy only:

```
bazelcredhelper.Resolver(ctx)
└─ live.Resolver.Resolve(ctx, envs)
   ├─ !origin.StoreManaged()  ⇒ Credential{Token}            no expiry hint
   ├─ cred.Expiry.IsZero()    ⇒ warnStale, Expiry = now+1m   soft cache miss
   └─ else                    ⇒ Expiry = cred.Expiry - expiresLead
```

### `status` and `doctor` — read-only

```
cmd/common.currentAuthStatus
└─ live.Resolver.ResolveNoRefresh(envs)                              L4
   ⇒ Credential, Origin
└─ live.Describe(cred, origin)         pure formatting, no I/O
   ├─ origin.Label()                                                 L0
   ├─ cred.WorkspaceID
   └─ cred.Expired()                                                 L0
```

Both the doctor's backend probe and the invocation PUT resolve through this same
path, which is what stops them disagreeing about which credential is current.

## Adding to this

**A new consumer** calls `live.Resolve` (or `ResolveNoRefresh` for read-only
diagnostics, `ResolvePinned` for an activation path). It does not read the keychain,
does not read the config file, and does not call `oauth.EnsureFreshFrom`.

**A new backend** implements `store.Store` and is added to `SelectAuto` and the
`loadFrom` chain. It stores `auth.TokenSet` — no new credential type.

**A new field on a credential** goes on `TokenSet` if it is persisted, and on
`Credential` only if a consumer needs it to make a call. Most new fields are the
former.

**A new precedence rule** goes inside `live.resolve`. If it applies to one caller
only, it is a `Prefer` variant, not a second function. If you are writing a second
resolve function, stop.

## Enforcement

`make lint-arch` runs alongside `golangci-lint` in `make check` and fails on:

1. any `bitrise-build-cache-cli` import inside `internal/auth/*.go`
2. `auth.TokenSet` referenced outside `internal/auth/…` and `internal/config/multiplatform`
3. `internal/config/common` imported from anywhere under `internal/auth/`
4. `keychain.New(`, `store.New{Keychain,File}(` or `oauth.EnsureFreshFrom(` called
   from L5, outside the login, logout and clear commands

These are greps, not a type system — cheap to run, and they are what keeps the
layering from eroding one convenient import at a time.
