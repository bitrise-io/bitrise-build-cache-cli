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

Bitrise warms VMs up before assigning work to them. Warmup already installs the
CLI and runs `activate gradle-mirrors`. If activation could also wire up Build
Cache there, every build on a warmed VM would get it with no Step at all.

Warmup has none of what activation currently relies on:

| Available at warmup | Not available at warmup |
|---|---|
| VM env (`BITRISE_DEN_VM_DATACENTER`, stack rev) | auth token, JWT |
| host facts (OS, cores, memory) | workspace / app / build / workflow |
| the CLI binary | `envman`, `BITRISE_IO`, a git checkout |

**The central hazard is not that warmup knows too little — it is that anything it
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

| Tool | Warmup writes | Resolved at build time, by |
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
# bitrise-build-cache: activated at VM warmup (lite)
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

The storage helper is deliberately **not** started at warmup: it would bind a
`$TMPDIR`-scoped socket, take the non-CI idle timeout, and freeze metadata
resolved before any build existed.

For the same reason lite persists no `ipcEndpoint` and no `idleTimeout`;
`ReadConfig` re-resolves an absent value through the same resolver the writer
uses, so every reader gets its own build's answer. Without this, macOS builds
would point at a dead socket while Linux passed, because both resolve `/tmp`.

## Benchmark phase

The phase query is keyed on workspace + app + workflow, so warmup cannot make it.
The build does, through `internal/config/common.ResolveBenchmarkPhase`:

1. `BITRISE_BUILD_CACHE_BENCHMARK_PHASE_<TOOL>` — an explicit override, used by
   e2e workflows. Never recorded, so a pin cannot outlive the build that set it.
2. The record at `~/.local/state/xcelerate/benchmark/benchmark-phase-<tool>.json`,
   **if it belongs to this build**.
3. The API — once. The answer is recorded for the build's other invocations.

The record is build-scoped because a persistent runner reuses the machine:
unscoped, one build's `baseline` would disable the cache for every build after
it. `"no phase"` is recorded too, or an ordinary build pays for a request per
invocation.

When two invocations race, both write, and the one that lands second keeps the
higher-ranked phase (`baseline` > `warmup` > none). Baseline wins because half a
build caching and half not would make the measurement meaningless.

Callers:
- **xcode** — the wrapper resolves before starting the proxy, because on baseline
  there should be no proxy at all.
- **gradle and others** — `bitrise-build-cache benchmark-phase --tool <tool>`, at
  **execution** time. Not at Gradle configuration time: the phase changes per
  build, and anything read during configuration becomes a configuration-cache
  input, which invalidates the entry on every build.

## Testing it

`scripts/preboot_emulate.sh <tool>` runs `activate --lite` with the build-scoped
variables stripped from the child environment, and does the two jobs that belong
to the VM: installing the CLI somewhere the build can find it, and putting the
wrapper dir on `PATH`. Every e2e workflow activates through it.

It is a **denylist**, and cannot be made airtight: the script never leaves the git
checkout, so a repo URL still resolves. The property is proved by
`scripts/assert_no_build_metadata.sh`, which checks the generated configs for the
values themselves and fails if it ran no substantive check.

## Known limits

- **Containerised builds.** Warmup cannot know the build will run in another
  filesystem namespace. A Bazel build inside Docker needs the CLI mounted in, or
  the credential helper cannot be spawned and the build aborts. Loud, not silent.
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
