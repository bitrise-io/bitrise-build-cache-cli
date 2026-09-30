# Lite activation (`activate --lite`)

`bitrise-build-cache activate <tool> --lite` configures a machine for Build Cache
**before a build is assigned to it**. It writes the static wiring a build tool
needs and defers everything else — credentials, build identity, benchmark phase —
to the moment the build tool actually runs.

Ticket: ACI-5515.

## Why

Build Cache normally requires a Step. A Step runs inside a build, so it can see
the workspace, the app, the workflow and a token, and it bakes what it learns
into `~/.gradle/init.d/...`, `~/.bazelrc` and `~/.bitrise-xcelerate/config.json`.

Bitrise warms VMs up before assigning work to them. Preboot already installs the
CLI and runs `activate gradle-mirrors`. If activation could also wire up Build
Cache there, every build on a warmed VM would get it with no Step at all.

Preboot has none of what activation currently relies on:

| Available at preboot | Not available at preboot |
|---|---|
| VM env (`BITRISE_DEN_VM_DATACENTER`, stack rev) | auth token, JWT |
| host facts (OS, cores, memory) | workspace / app / build / workflow |
| the CLI binary | `envman`, `BITRISE_IO`, a git checkout |

**The central hazard is not that preboot knows too little — it is that anything it
writes is inherited by whatever unrelated build later lands on that VM.** A baked
app slug means one customer's build reports as another's. A baked token means one
workspace's build authenticates as another's.

## What `--lite` guarantees

Under `--lite` the CLI:

- keeps **no credential** and **no build identity** — both are discarded at the
  config boundary, so the guarantee does not depend on the environment happening
  to be empty;
- pins nothing to disk, queries no benchmark phase;
- starts no daemon and no ccache storage helper;
- uses no `envman` — that belongs to a build;
- persists no machine-scoped policy (`--cache-push`, `--project-mode`).

A malformed credential is still an error. Only *absence* is tolerated
(`live.Resolver.ResolveAllowingNone`).

**Deferral is a lite-only behaviour.** A normal activation knows which build it
belongs to and keeps baking exactly what it always did. Nothing here changes for
existing users of the CLI.

## How each tool defers

| Tool | Preboot writes | Resolved at build time, by |
|---|---|---|
| gradle | init script, `gradle.properties` block | the plugins (`auth token`, env) |
| bazel | bazelrc: helper line, endpoints, host metadata, lite marker | `bitrise-build-cache get` |
| xcode | `config.json`, `xcodebuild`/`xcrun` wrappers, CLI copy | the wrapper, per build |
| ccache | `config.json` (no helper started) | the RN runner / storage helper |

### gradle

The init script already carried no cache or analytics token — the plugins fetch
it mid-build. Lite additionally omits `providerName` and `appSlug`, which the
plugin then resolves from the build's own environment.

An **empty** value is not the same as an absent one: `providerName.set("")` is a
present-but-empty property, and it stopped the plugin's own `orElse(...)`
fallback from ever firing. Every warmed-up VM would have reported CI builds as
local. The template omits the setter entirely instead.

Byte-stability matters here: the init script is a configuration-cache input,
compared by content hash. Baking a per-build JWT is what historically defeated
cross-build config-cache reuse. Lite bakes no token at all, so the file is
identical on every VM.

### bazel

Per-invocation headers — `x-org-id`, `x-app-id`, `x-workflow-name`,
`x-flare-build-id` / `x-build-id`, `x-ci-provider`, `x-flare-builduser` — used to
be `--remote_header` / `--bes_header` lines in the bazelrc. Under lite they are
omitted and returned by the credential helper instead, resolved against the
build's own environment.

Exactly one side emits each key. Both sides read the same signal:

```
# [start] generated-by-bitrise-build-cache
# bitrise-build-cache: activated at preboot (lite)
build --credential_helper=*.services.bitrise.io=bitrise-build-cache
...
```

The marker lives **inside the generated block**, written by the same atomic write
as the config it describes, so the file that tells the helper to stay quiet and
the file that describes the configuration cannot disagree. `get` also uses it to
tell "this machine never had a credential" (ordinary — exit 0, empty headers,
one line on stderr) from "this machine's credential has gone missing" (a
misconfiguration — fail loudly and point at the doctor).

`x-repository-url` is not part of this: it was helper-resolved before lite
existed, and follows the helper rather than the lite flag.

### xcode

Auth was already deferred — the config persists no credential and the wrapper
re-resolves per build. Lite adds: no `envman` (so putting the wrapper dir on
`PATH` becomes the VM's job), and no frozen `proxySocketPath`.

A lite config is treated as *no usable config* by the next real activation. That
matters for more than staleness: the carry-forward guard also holds the
`isXcelerateInPath` safety net, and skipping it let `which xcodebuild` resolve to
the wrapper and persist it as the *original* — the wrapper would then exec
itself.

### ccache

The storage helper is deliberately **not** started at preboot: it would bind a
`$TMPDIR`-scoped socket, take the non-CI idle timeout, and freeze metadata
resolved before any build existed.

For the same reason lite persists no `ipcEndpoint` and no `idleTimeout`;
`ReadConfig` re-resolves an absent value through the same resolver the writer
uses, so every reader gets its own build's answer. Without this, macOS builds
would point at a dead socket while Linux passed, because both resolve `/tmp`.

## Benchmark phase

The phase query is keyed on workspace + app + workflow, so preboot cannot make it.
The build does, through `internal/config/common.ResolveBenchmarkPhase`:

1. `BITRISE_BUILD_CACHE_BENCHMARK_PHASE_<TOOL>` — an explicit override, used by
   e2e workflows. Never recorded, so a pin cannot outlive the build that set it.
   The activation path (`config/{gradle,xcelerate}.ApplyBenchmarkPhase`) skips
   the record for the same reason, since the provider hands an override back
   indistinguishable from an API answer.
2. The record at `~/.local/state/xcelerate/benchmark/benchmark-phase-<tool>.json`,
   **if it belongs to this build**.
3. The API — once. The answer is recorded for the build's other invocations, and
   so is a failure, as "no phase": the client retries three times against a 10s
   timeout, and one build runs the tool dozens of times.

The record is build-scoped because a persistent runner reuses the machine:
unscoped, one build's `baseline` would disable the cache for every build after
it. `"no phase"` is recorded too, or an ordinary build pays for a request per
invocation. Every other reader of the file — `LogBenchmarkSummary`, the React
Native activator, the Gradle plugin — reads only `phase`, so a resolve for a
different build deletes the record it found rather than leaving it readable.

When two invocations race, both write, and the one that lands second keeps the
higher-ranked phase (`baseline` > `warmup` > none). Baseline wins because half a
build caching and half not would make the measurement meaningless. The merge is
build-scoped too: an unscoped record (off CI, or a CI provider with no build ID)
is never merged into or reused.

Callers:
- **xcode** — the wrapper resolves before starting the proxy, because on baseline
  there should be no proxy at all. Only for a build action with the cache still
  enabled: a fastlane or CocoaPods run fires dozens of `-version` /
  `-showBuildSettings` calls, and the phase cannot change any of them.
- **gradle** — the generated init script resolves the phase through a
  `ValueSource` that shells out to `bitrise-build-cache benchmark-phase --tool
  gradle`, and a `baseline` answer turns the cache off for real: the remote
  cache and `BitriseCCachePlugin` are skipped, and Gradle's local cache is left
  at its default, which is exactly the shape a full activation produces when it
  resolves `baseline` itself.

  A `ValueSource` rather than `providers.environmentVariable`, because Gradle
  re-runs `obtain()` on every build — configuration-cache hits included — and
  only invalidates the entry when the value it returns actually changes. A
  stable phase therefore costs one CLI call and keeps the entry; a phase that
  flips invalidates it, which is correct, because the build genuinely caches
  differently. Verified against Gradle 9.3: two consecutive
  `--configuration-cache` runs reuse the entry on both `baseline` and `warmup`.

  Emitted only under `--lite`. A full activation has already resolved the phase
  and baked the result into the file, so a second query per build would be
  waste — and off CI it would run `benchmark-phase` on a developer's machine.
- **other tools** — `bitrise-build-cache benchmark-phase --tool <tool>`, at
  execution time.

  **The Gradle plugins do not call this yet**, so a lite Gradle build currently
  resolves no phase at all. The subcommand is the intended interface; wiring it
  is a plugin-side change.

## Entitlement

A workspace with no Build Cache trial or subscription should never be activated.
The build cannot use the cache, and activating anyway spends an analytics
invocation recording that fact. `SkipActivationForEntitlement` stops before
anything is written and points the user at the trial instead.

It is deliberately three-valued. "We could not tell" is not "no" — otherwise one
website outage disables caching for everyone. Only an explicit negative skips.

Lite never skips: preboot has no workspace to ask about, so the question moves to
build time with everything else. On Bitrise CI the JWT is injected for every
workspace regardless of entitlement, so a lite-warmed VM currently wires up for
everyone and the decision falls to the backend rejecting.

### The gate is an optimisation, not the safety net

Every tool already survives an unauthorised backend without failing the build,
and that stays the real guarantee — the gate is best-effort and can be wrong.
What differs is how much each one spends finding out:

| Tool | On `UNAUTHENTICATED` |
|---|---|
| Xcode | the first RPC fails the capabilities check, and compilation caching degrades to plain compilation |
| Gradle | the plugin throws instead of burning its retries, and Gradle drops the remote cache for the rest of the build |
| ccache | re-checks on **every** connection, so it never stops asking |
| Bazel | no CLI-side handling; the helper returns empty headers with no credential, otherwise Bazel's own remote-cache error handling applies |

ccache's retry is deliberate — a credential refreshed mid-build has to start
working — but it cannot tell "this token just expired" from "this workspace has
no Build Cache", and those want opposite answers. So on an unentitled workspace
it re-asks and re-fails once per compile.

That is the strongest argument for the gate: standing down keeps the build
green, it does not make the attempt free. The gate is what makes it free.

> **TEMPORARY — the endpoint does not exist yet.**
> `BITRISE_BUILD_CACHE_TMP_SKIP_ENTITLEMENT_CHECK` disables the gate, and this
> repo's own `bitrise.yml` sets it, or every e2e activation would refuse to run.
>
> When the endpoint ships, flip `entitlementEndpointShipped` in
> `internal/config/common/entitlement.go`. A unit test then fails and names
> everything to delete: the env var, the branch that reads it, the `bitrise.yml`
> entries, this section, and the test itself. The flip and the cleanup cannot
> drift apart.

## Local-dev features are mutually exclusive with lite

Project scoping (`--project-mode opt-in`) answers "did this developer mark this
checkout?". A warmed-up VM has no developer and no checkout, and runs whatever
build it is handed — so the question has no meaning there. Opt-in also renders a
scope-check `ValueSource` that shells out to the CLI on *every* Gradle
configuration.

So `--lite --project-mode …` is refused outright rather than silently ignored,
and a lite activation resolves `always` regardless of what an earlier activation
persisted on the machine. Lite never writes machine-wide policy either; it only
reads the cache-push setting, falling back to the built-in default.

## What the VM has to hand the build

Activation normally publishes several values through `envman`, which belongs to
a build and does not exist at preboot. These are all *machine-scoped* — a warmed
VM genuinely knows them, it just has no build to give them to yet — so
publishing them is the VM's job, not the CLI's:

| Value | Who needs it | Stand-in |
|---|---|---|
| `PATH` + the wrapper dir | `xcodebuild` / `xcrun` interception | `/etc/paths.d`, the agent env, or `preboot_emulate.sh` |
| the CLI on `$PATH` | Gradle plugins' token lookup, Bazel's credential helper | install to `/usr/local/bin` |
| `BITRISE_XCODE_DERIVED_DATA_PATH` | cache steps targeting the SPM checkouts | `xcelerate derived-data-path`, exported by the VM |

`BITRISE_BUILD_CACHE_CLI` is deliberately *not* on that list: the plugins fall
back to `$PATH`, which the CLI install already covers.

`scripts/assert_lite_runtime_wiring.sh` checks these reached the build, and
checks **positives** — the DerivedData root exists, agrees with the CLI's own
answer, and has been populated. Absence assertions are not enough here: this gap
first shipped green because a cache step's glob expanded to `/*/SourcePackages`,
matched nothing, and reported success.

## Testing it

`scripts/preboot_emulate.sh <tool>` runs `activate --lite` with the build-scoped
variables stripped from the child environment, and does the jobs that belong to
the VM: installing the CLI somewhere the build can find it, putting the wrapper
dir on `PATH`, and exporting the DerivedData root. Every e2e workflow activates
through it, except two that deliberately do not: `feature-e2e-gradle-7` is the
non-lite regression guard, and `ccache-storage-helper-test` needs the envman
delivery lite skips.

It is a **denylist**, and cannot be made airtight: the script never leaves the git
checkout, so a repo URL still resolves. The property is proved by
`scripts/assert_no_build_metadata.sh`, which checks the generated configs for the
values themselves and fails if it ran no substantive check.

## Rollout plan

Staged so that each step limits the blast radius of the one before it. Nothing
here is shipped by this PoC beyond step 1's CLI half.

0. **Website: entitlement endpoint, per tool.** The gate needs an answer keyed on
   workspace *and* build tool, so a workspace can be turned on for Gradle without
   also being on for Xcode. Until it ships the check is bypassed by
   `BITRISE_BUILD_CACHE_TMP_SKIP_ENTITLEMENT_CHECK` — see
   [Entitlement](#entitlement). The bypass is not removed here — it stays until
   step 6, so the gate can be turned on per workspace while it is being tested.
1. **CLI + plugins: ship lite mode behind an org-slug env gate.** Released, but
   inert: lite only engages for an allowlist of workspace slugs carried in an env
   var, so internal workspaces can exercise it while everyone else is untouched.
   The gate is evaluated **at build time**, not at preboot — preboot does not know
   the workspace, which is the whole premise of lite mode.
2. **Preboot: enable the tool in the startup-script extension.** Add the
   `activate <tool> --lite` call to `build-prebooting-deployments`. Every VM now
   boots activated; the org-slug gate is what keeps it a no-op for workspaces not
   on the list.
3. **Test the whole flow per tool.** With a trial, with an active subscription,
   and with no entitlement at all — the last one is the case that must produce no
   failed build, no auth-error spam, and no analytics invocation.
4. **GTM heads-up.** Cache activity starts appearing for workspaces that never
   added a Step, which changes what support and sales see.
5. **Remove the org-slug gate.** General availability for lite mode itself.
6. **Remove the entitlement bypass.** Delete
   `BITRISE_BUILD_CACHE_TMP_SKIP_ENTITLEMENT_CHECK`, flip
   `entitlementEndpointShipped` to `true`, and drop the artefacts
   `TestEntitlementBypass_MustBeRemovedOnceTheEndpointShips` names — that test
   fails on the flip precisely so this step cannot be forgotten. Also unset the
   variable in `bitrise.yml`. Only after this is the entitlement endpoint the
   sole thing deciding whether a build caches.

Steps 0 and 1 are independent and can run in parallel; 2 must not precede 1, or a
VM would activate for workspaces the gate was meant to exclude. 6 is last rather
than folded into 0 because the bypass is what lets steps 1-5 run while the gate
is still being validated — but the PoC is not finished until it is gone.

## Known limits

- **Containerised builds need two things handed in.** Preboot cannot know the
  build will run in another namespace.
  1. *The binary.* A Bazel build inside Docker needs the CLI mounted in, or the
     credential helper cannot be spawned and the build aborts. Loud, not silent.
     Mount it from a path the host daemon shares — the step container's own
     `/usr/local/bin` is not one.
  2. *The build's identity.* Lite resolves the per-invocation metadata where the
     helper runs, so the container needs the env that identifies the build —
     `BITRISE_IO` (which gates CI detection) and `BITRISE_TRIGGERED_WORKFLOW_ID`
     among them. A full activation baked these on the host, so a container that
     never received them still reported correctly; under lite it reports
     `ciProvider: unknown`. Quiet, and only visible in the invocation record.
- **Non-wrapped ccache builds.** `CCACHE_*` and `CCACHE_BASEDIR` need a build to
  exist and shipped via `envman`. The React Native path is fine — its runner
  applies them at build time — but a bare `activate c++ --lite` followed by a
  direct `ccache`/`cmake` invocation gets no remote cache.
- **Entitlement.** A resolved credential currently means the cache is attempted.
  On Bitrise CI the JWT is injected for every workspace, entitled or not, so the
  decision falls to the backend rejecting. Pending a dedicated endpoint.
- **BES on an unentitled workspace.** Unverified: whether
  `--bes_upload_mode=wait_for_upload_complete` fails the build when the helper
  returns empty headers. If it does, lite must not emit `--bes_backend` without a
  credential.
