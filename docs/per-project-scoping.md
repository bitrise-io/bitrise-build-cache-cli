# Per-project workspace scoping

## Overview

The CLI's `activate` commands are machine-wide: they wire Gradle, Bazel, Xcode
and ccache to whichever workspace was last activated on the host. That is fine
when every project on the machine belongs to the same workspace, and awkward
when it does not. Per-project scoping lets a single machine service several
workspaces without re-running `activate` between projects.

The mechanism is a small JSON file — `.bitrise-build-cache.json` — committed
at the project root. Every tool reads it at build time and routes that
project's cache traffic to the workspace declared in the marker, picking a
credential the CLI stored for that workspace instead of the machine-wide one.

**One-sentence summary.** Put `.bitrise-build-cache.json` at your project root
with `{"workspace": "<slug>"}`; the CLI activates the workspace's credential
automatically when that project builds. No marker means today's behaviour —
the machine-wide credential is used unchanged.

---

## Marker file schema

The marker is JSON at the project root:

```json
{
  "workspace": "acme-corp",
  "push": true,
  "tools": {
    "gradle": { "enabled": true },
    "bazel":  { "enabled": true },
    "xcode":  { "enabled": true },
    "ccache": { "enabled": false }
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `workspace` | string | yes | The Bitrise workspace slug this project's cache traffic belongs to. |
| `push` | bool | no | Reserved for a future per-project push/pull override. Ignored by Phase 1 tools; set only when documented for the tool you are using. |
| `tools` | object | no | Optional per-tool override map. Only `enabled` is defined; a missing entry means "follow the global activation". |
| `tools.<name>.enabled` | bool | no | Reserved. Consumers may honour this to skip enabling one tool for this project; unspecified means "follow the global activation". |

Unknown top-level fields and unknown `tools.<name>` keys are tolerated so a
newer marker still parses on an older CLI. A marker missing the `workspace`
field is a hard error — the CLI reports it and falls back to the machine-wide
credential rather than guess.

---

## How each tool consumes it

### Gradle

- **Read from:** the Gradle project root, i.e. `settings.rootDir`. Composite /
  included builds each check their own `settings.rootDir`; outer-root markers
  do not cascade.
- **Where in the pipeline:** the generated init script
  (`~/.gradle/init.d/bitrise-build-cache.init.gradle.kts`) declares a
  `providers.fileContents(...)` on the marker so the configuration cache
  invalidates on marker edits, then shells out to
  `bitrise-build-cache workspace-for --path <settings.rootDir>` inside
  `settingsEvaluated { ... }`.
- **On match:** the resolved workspace slug is passed to
  `providers.bitriseAuthToken(<slug>)`, which in turn invokes
  `bitrise-build-cache auth token --workspace=<slug>`. The build cache and
  analytics blocks fill in that token.
- **On no match:** the init script gets an empty slug back and calls
  `providers.bitriseAuthToken("")`, which resolves the machine-wide token —
  today's behaviour.
- **IDE / daemon caveat:** because the marker is declared as a
  `ValueSource` input via `providers.fileContents(...)`, warm Gradle daemons
  (JetBrains / Android Studio) pick up marker edits on the next configuration.
  Any tool that bypasses configuration caching still evaluates the init
  script every build.

### Bazel

- **Read from:** the credential helper's CWD, walking up until the first
  `.bitrise-build-cache.json` is found. Bazel invokes the credential helper
  from the Bazel workspace root, so putting the marker next to `WORKSPACE` /
  `MODULE.bazel` is the reliable placement. Sub-directory markers work when
  the helper walks up to reach them, but Bazel's per-URI credential cache is
  keyed to the workspace root and does not re-fetch on CWD changes inside the
  same workspace.
- **Where in the pipeline:** `~/.bazelrc` names the CLI as
  `--credential_helper`; per RPC, Bazel invokes it and reads back a
  `GetCredentialsResponse` with an `Expires` hint.
- **On match:** the helper resolves the per-workspace credential for the slug
  in the marker and returns it in the `Authorization` header. The `Expires`
  hint is `min(credentialExpiry, now + 5m)` — Bazel caches the response until
  that instant, so marker edits are picked up within five minutes without
  spawning the helper per RPC.
- **On no match:** the helper falls back to the machine-wide credential.
- **Malformed / unknown slug:** warned once on stderr, the machine-wide
  credential still resolves — the credential helper is a build-time
  dependency and hard-failing there would take the build down.

### Xcode

- **Read from:** the wrapper resolves the Xcode project directory from
  `-project` / `-workspace` (or CWD) and walks up from there.
- **Where in the pipeline:** `cmd/xcode/xcodebuild.go` runs
  `resolveWorkspaceScope` before injecting the cache flags into
  `xcodebuild`. When a marker match resolves a stored per-workspace
  credential, the wrapper swaps `Config.AuthConfig` and `Config.AuthOrigin`
  so the `PutInvocation` payload's `BitriseOrgSlug` reflects the marker's
  workspace, not the machine-wide one.
- **Proxy:** the xcelerate proxy stays global. Per-session routing to the
  right workspace happens via the `x-bitrise-workspace-id` metadata header
  the wrapper attaches to its `SetSession` RPC; the proxy re-resolves the
  credential for that workspace before it hits the KV backend.
- **On no match:** the wrapper injects the machine-wide credential exactly
  as before.

### ccache

- **Read from:** the compile-time working directory, walking up. Read at
  activation time (when `activate c++` or `activate react-native` runs) and
  again per compile session by the wrapper that talks to the storage helper.
- **Where in the pipeline:** `activate` writes the discovered slug into the
  ccache config (`~/.bitrise/cache/ccache/config.json`) and exports
  `BITRISE_BUILD_CACHE_WORKSPACE_ID` via envman so a compile in that shell
  authenticates against the same workspace. The storage helper receives the
  slug per session over the V2 IPC opcode
  `0xB4 SetInvocationIDWithWorkspace` — the pre-existing `0xB1`
  `SetInvocationID` opcode still works for older wrappers so mixed installs
  keep resolving to the machine-wide credential.
- **On no match:** the empty slug leaves the helper on the machine-wide
  credential.
- **Daemon caveat:** the storage helper is long-running. Marker edits are
  picked up when the wrapper opens a new session (each build / compile
  cycle), not while a session is already open. Restart the helper
  (`bitrise-build-cache daemon restart ccache`) after the very first time
  you add a marker so its cached machine-wide credential is dropped.

---

## Scenarios A/B/C: worked examples

Three shapes cover how the auth store is set up. See
[`docs/auth.md`](./auth.md#scenario-matrix) for the summary table; the walkthroughs
below are the end-to-end recipes.

### Scenario A — machine-wide only

Every build on this host uses one credential.

```sh
bitrise-build-cache auth set --token <machine-token> --workspace-id <ws>
bitrise-build-cache auth status   # prints "Scenario: machine-wide only"
bitrise-build-cache activate gradle
```

No project marker required. This is the pre-per-project-scoping shape.

### Scenario B — per-workspace only

The machine holds only per-workspace tokens; every project must have a marker
that names one of the workspace slugs, or the build fails with an actionable
error.

```sh
# Seed one slug per workspace you want to build against.
bitrise-build-cache auth clear
bitrise-build-cache auth set --token <acme-token> --workspace-id acme
bitrise-build-cache auth set --token <widgets-token> --workspace-id widgets
bitrise-build-cache auth status   # prints "Scenario: per-workspace only"

# Inside an acme project directory:
cat .bitrise-build-cache.json      # {"workspace":"acme"}
bitrise-build-cache activate gradle    # succeeds; no auth baked into the init script
```

`activate {gradle,bazel,xcode,ccache,reactnative}` all treat this state as
"credential resolves per build" — nothing gets baked into the generated config.
The Gradle ValueSource, Bazel credential helper, Xcode wrapper, and ccache proxy
each call `auth token --workspace=<slug>` (or its in-process equivalent) with
the marker's slug at build time.

### Scenario C — both (machine-wide as fallback)

Migration shape: keep the machine-wide credential as the fallback while seeding
per-workspace tokens.

```sh
bitrise-build-cache auth set --token <machine-token> --workspace-id default-ws
bitrise-build-cache auth set --token <acme-token> --workspace-id acme
bitrise-build-cache auth status   # prints "Scenario: both (machine-wide as fallback)"
```

Inside a project whose marker names `acme`, the acme token is used. Inside a
project with no marker (or a marker naming a slug that is not in the store), the
build falls back to the machine-wide credential — `doctor project-scope` WARNs
with the `auth set --token <token> --workspace-id <slug>` seed command when the
marker's slug is unknown.

## Setting up per-workspace credentials

The auth store holds a per-workspace map alongside the machine-wide
credential; see [`docs/auth.md`](./auth.md) for the store shape. `auth set`
routes on `--workspace-id`:

```sh
# Machine-wide credential — no --workspace-id.
bitrise-build-cache auth set --token <machine-token>

# Per-workspace credential — --workspace-id names the slot.
bitrise-build-cache auth set --token <acme-token> --workspace-id acme
```

`--workspace-id <slug>` populates the per-workspace map under that slug and
leaves the machine-wide slot alone. Repeat for each workspace to seed
several slots on the same host; the resolver picks a per-workspace entry
only when the marker asks for that slug.

Verify what is stored:

```sh
bitrise-build-cache auth token --workspace=<slug>
```

This prints the token that will be used for `<slug>` (falling back to the
machine-wide one with a warning if the slug is not in the store).

---

## Debugging with `doctor`

`bitrise-build-cache doctor` includes a `project-scope` check that walks up
from the current directory and reports which workspace it resolves to:

| State | When | What it means |
|---|---|---|
| **OK** | No marker found up to the filesystem root. | Machine-wide credential in use — today's behaviour, no per-project scoping. |
| **OK** | Marker found and a per-workspace credential exists for its slug. | The per-workspace credential will be used. |
| **WARN** | Marker found but no per-workspace credential exists for its slug. | Falls back to the machine-wide credential. Seed the credential with `auth set --token <token> --workspace-id <slug>`. |
| **ERROR** | Marker present but malformed or missing the `workspace` field. | The build falls back to the machine-wide credential; fix or delete the marker. |

Run `doctor` inside a project directory to exercise the walk-up. The check
reports the marker's path and, when the marker's `tools` block is set, which
tools are declared enabled / disabled — useful for verifying a monorepo
subdirectory picks up the marker at the ancestor you expect.

---

## Phase 1 posture: no opt-in required

This release ships the marker-reading pipeline with an **allow-all default**:
absent marker means the machine-wide credential is used, exactly as today.
Nothing changes for hosts that do not adopt the marker; the only behaviour
change is that a marker, when present, wins over the machine-wide default
for the tools it names.

A subsequent release will introduce a config flag to flip the default to
**opt-in** — marker absent means cache off — for hosts that want strict
per-project control. That change is out of scope here and will be documented
in its own release notes.

---

## Multi-workspace example

Two projects on the same laptop, each in its own workspace:

```
~/src/acme-web/               ← workspace: acme
~/src/acme-web/.bitrise-build-cache.json  → {"workspace": "acme"}

~/src/widgets-mobile/         ← workspace: widgets
~/src/widgets-mobile/.bitrise-build-cache.json  → {"workspace": "widgets"}
```

One-off seeding (per workspace):

```sh
bitrise-build-cache auth set --token <acme-token>    --workspace-id acme
bitrise-build-cache auth set --token <widgets-token> --workspace-id widgets
```

Each command populates the per-workspace map under its own slug, leaving
the other workspace's slot (and any machine-wide credential) untouched. From
that point on every build under `~/src/acme-web` authenticates against
`acme` and every build under `~/src/widgets-mobile` authenticates against
`widgets`, on the same machine, with no `activate` in between. A project
without a marker (say `~/src/random-experiment`) resolves the machine-wide
credential — if one is set with `auth set --token <t>` (no `--workspace-id`)
or `auth login`. When only per-workspace slots exist, a marker-less project
sees no credential and the cache stays off.

---

## Use build cache only in specific projects

The Phase-1 default is allow-all: a marker resolves through its slug, and a
project without a marker falls back to the machine-wide slot. That's fine
for laptops that opt every project in, but leaks cache traffic to the
machine-wide workspace on hosts that only want the cache in a few named
projects.

Until the Phase-2 opt-in flag lands (see below), the way to get "cache only
in project X, off everywhere else" is to seed the per-workspace map without
setting a machine-wide slot:

1. If you already have a machine-wide credential, clear it so no marker-less
   project has a credential to fall back to:
   ```sh
   bitrise-build-cache auth clear
   ```
   `auth clear` removes every stored credential — the browser login, the
   machine-wide slot, and the per-workspace map — from the OS keychain and
   the config file. If you only want to remove the browser login and keep a
   manually-set token in place, use `auth logout` instead — it is narrower
   by design.
2. Add a marker at each project you want the cache in:
   ```json
   { "workspace": "<slug>" }
   ```
3. Seed the per-workspace map for each marker slug — the machine-wide slot
   stays empty because `--workspace-id` routes to the per-workspace map
   only:
   ```sh
   bitrise-build-cache auth set --token <acme-token> --workspace-id acme
   ```

From that point on: builds under a marker whose slug is in the map
authenticate with that workspace's token; builds without a marker (or under
a marker whose slug isn't in the map) resolve nothing from the store and
the cache stays off (Gradle/Bazel/Xcode/ccache activation still runs, but
the auth resolver has nothing to hand out).

`bitrise-build-cache doctor` surfaces this state directly. Inside the
marker-owning project the `project-scope` check reports the resolved slug;
outside it, the `auth` check reports no stored credential.

> **Note.** A subsequent release will add a first-class opt-in mode where
> "marker absent means cache off" is the default without having to leave
> the machine-wide slot empty — see
> [Phase 1 posture: no opt-in required](#phase-1-posture-no-opt-in-required).

---

## Roll back a machine-wide activation

The `activate` subcommands don't have a `deactivate` counterpart. To undo a
prior `bitrise-build-cache activate {gradle|bazel|xcode|c++|react-native}`,
delete the generated config the CLI owns:

- **Gradle** — delete `~/.gradle/init.d/bitrise-build-cache.init.gradle.kts`
  (whole file, owned by the CLI), then remove the block delimited by
  `# [start] generated-by-bitrise-build-cache` /
  `# [end] generated-by-bitrise-build-cache` in
  `~/.gradle/gradle.properties`.
- **Bazel** — remove the block delimited by the same start/end markers in
  `~/.bazelrc`. `.bazelrc` itself is not owned by the CLI, so leave any
  hand-written lines around the block in place.
- **Xcode** — delete `~/.bitrise-xcelerate/config.json` (whole file, owned
  by the CLI), stop the xcelerate proxy with
  `bitrise-build-cache xcelerate stop-proxy`, and remove the wrapper-PATH
  export written to `~/.bashrc` / `~/.zshrc` under the
  `# [start] Bitrise Xcelerate` / `# [end] Bitrise Xcelerate` marker block.
- **ccache** — delete `~/.bitrise/cache/ccache/config.json` (whole file,
  owned by the CLI) and stop the storage helper with
  `bitrise-build-cache ccache storage-helper stop`. The `CCACHE_*` env
  vars the activator writes travel through envman on CI and through the
  build's own environment locally — they are not persisted to shell rc
  files, so a new shell has them cleared already.
- **React Native** — the wrapper activates Gradle + Xcode + ccache in turn.
  Follow each of the three sections above; there is no single React Native
  file to remove.

If you installed the LaunchAgents (macOS) or systemd --user units (Linux)
that supervise the sidecars, `bitrise-build-cache daemon uninstall` stops
and removes them in one call — safe to run whether or not the units are
present.

After the on-disk config is gone the stored credentials still live in the
keychain / config file; run `bitrise-build-cache auth clear` if you also
want the tokens removed.

---

## Troubleshooting

- **`doctor project-scope: WARN … no per-workspace token is stored`.** The
  marker names a workspace slug the auth store does not know about. Seed it
  with `auth set --token <token> --workspace-id <slug>`. The build still
  runs — the machine-wide credential is used.
- **`doctor project-scope: ERROR … marker is malformed`.** The marker file
  cannot be parsed, or the required `workspace` field is missing. Fix the
  JSON or delete the marker; the build falls back to the machine-wide
  credential in the meantime.
- **Marker edit not picked up in an IDE Gradle daemon.** The init script
  declares the marker as a `ValueSource` input via
  `providers.fileContents(...)`, so the configuration cache invalidates on
  the next configure. Some IDE workflows re-use a warm daemon without
  reconfiguring; `./gradlew --stop` (or the IDE's "Restart Daemon" action)
  forces the next build to re-evaluate.
- **Bazel edit not picked up.** Bazel's credential helper cache TTL is up
  to five minutes (see the `Expires` cap above). A `bazel clean` clears the
  cache immediately; otherwise the next build after the window picks up the
  marker.
- **ccache still using the machine-wide credential after adding a marker.**
  The long-running storage helper caches the credential per session.
  Restart the helper (`bitrise-build-cache daemon restart ccache`) after
  the first-ever marker addition, then rebuild.
- **Marker at the wrong depth for Bazel.** Bazel's per-URI credential cache
  is keyed to the workspace root and does not re-resolve on CWD changes
  within the same workspace. Placing the marker next to `WORKSPACE` /
  `MODULE.bazel` is the reliable placement; a marker in a sub-directory
  works only when the helper walks up to reach it and the daemon has not
  cached a previous lookup.

---

## Related

- [`docs/auth.md`](./auth.md) — the per-workspace auth store, schema
  migration, and the resolver methods that back the marker-driven lookup.
- `docs/aci-5357-investigation-2026-09-01.md` and
  `docs/aci-5357-implementation-plan-2026-09-01.md` — the design docs this
  feature came out of.
