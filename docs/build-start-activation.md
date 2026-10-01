# Build-start activation (`activate all --auto`)

Ticket: ACI-5515. Getting Build Cache onto builds that never added a Step.

When a platform starts a build, the `bitrise` CLI runs
`bitrise-build-cache activate all --auto` once, before the first workflow. At that
point the build's credential, identity and environment already exist, so every
tool gets the same full activation a Step would have run and nothing has to be
deferred to the build tool.

## Architecture

Three components, each doing one job.

```
 preboot startup script            bitrise CLI (thin wrapper)           bitrise-build-cache
 ----------------------            --------------------------           -------------------
 pins CLI version + sha256   --->  installs that version:               activate all --auto
 exports the opt-in env,           host VM cache, else GAR,             1. resolve the credential
 the org allowlist and the         verified against the sha256          2. org gate (fail closed)
 host cache URL                    runs the CLI with the build's        3. entitlement (fail open)
                                   BITRISE_BUILD_CACHE_* envs and       4. activate each tool
                                   the services token only              hands back envman exports
                                   collects its envman exports
```

**Preboot** owns policy that is per VM: which CLI version, which organizations,
where the host cache is. It exports these into the DEN agent's environment, and
the agent already passes `BITRISE_*` variables through to `bitrise run`.

**The `bitrise` CLI** owns nothing about caching. In `bitrise run`, after the
build's envs are assembled and before the first workflow, `internal/buildcache`
does three things:

1. Skips unless `BITRISE_BUILD_CACHE_ACTIVATE_ALL=true`, and skips a nested
   `bitrise run` (the step execution id is already set).
2. Installs `bitrise-build-cache` at `BITRISE_BUILD_CACHE_CLI_VERSION`, checks the
   tarball against `BITRISE_BUILD_CACHE_CLI_SHA256`, and links it into
   `~/.bitrise/tools`, which is on the build's `PATH`. Source order is
   `BITRISE_BUILD_CACHE_CLI_HOST_CACHE_URL`, then the public GAR mirror.
3. Runs `bitrise-build-cache activate all --auto` with only the
   `BITRISE_BUILD_CACHE_*` envs and the services token from the build, under a
   two minute limit, and hands the CLI's `envman` exports to the first step.

A failure at any point is a warning, never a failed build. There is no version
pin in the `bitrise` repo: preboot already pins the version and sha256 and the
release automation already bumps them.

**The build cache CLI** owns every decision about whether and what to activate.
`activate all` runs `activate gradle`, `bazel`, `xcode` (macOS only) and
`react-native --gradle=false --xcode=false` (the C++ half) as separate processes,
so one tool failing or exiting cannot stop the others. Running them in-process
needs the `cmd/*` activators moved into `pkg/` first, which this change does not
need.

## The gates

The reason for gating is an *analytics flood*. Activating spends nothing by
itself, but every build that then runs a wrapped tool reports an invocation. The
analytics endpoint accepts an invocation from any workspace and does not reject
unentitled ones, so a workspace that never asked for Build Cache would fill the
invocation data. Nothing downstream of activation can decline, so the decision is
made before activation.

The gates live in the build cache CLI, not the `bitrise` CLI, so every consumer
gets the same behavior and the `bitrise` CLI stays a thin wrapper.

Two checks, in this order, both before anything is written:

1. **Org allowlist, `--auto` only, fails closed.**
   `BITRISE_BUILD_CACHE_AUTO_ACTIVATE_ORGS` holds workspace slugs separated by
   commas or whitespace, or `*`. The workspace comes from the build's own
   credential (`BITRISE_BUILD_CACHE_WORKSPACE_ID`, else the `org_id` claim of the
   services token). No credential, no workspace, no list, or a workspace not on
   the list all mean: log one line, write nothing, exit 0.
2. **Entitlement, fails open.** Every `activate <tool>` command, and `activate
   all` once for the whole set, asks whether the workspace has Build Cache for
   that tool and stops before writing anything on an explicit "no", printing where
   to start a trial. The answer is three-valued: an unreachable website, a missing
   workspace or an unexpected response is Unknown, and Unknown carries on, so a
   website outage cannot disable caching for everyone.

Without `--auto` (a person ran `activate all`) there is no allowlist: asking for
it explicitly is the consent. Entitlement still applies.

**The entitlement endpoint does not exist yet.** Until it ships, every answer is
Unknown and the check stops nothing, which makes the allowlist the only thing
protecting analytics. That is why the allowlist fails closed while entitlement
fails open. The allowlist is a rollout control and not a security boundary: the
workspace is read from the unsigned services token, and a build's own
`BITRISE_BUILD_CACHE_*` envs override the boot script's value, so a workspace can
put itself on the list from its own `bitrise.yml`. That only turns on for itself
what an activate Step already could, and the cache backend still authorizes the
real token on every request.

When the endpoint ships, flip `entitlementEndpointShipped` in
`internal/config/common/entitlement.go`. A unit test then fails and names what to
delete (the temporary bypass `BITRISE_BUILD_CACHE_TMP_SKIP_ENTITLEMENT_CHECK`, the
branch that reads it, and the test), so the flip and the cleanup cannot drift
apart. The request path in that file is provisional and will change when the
endpoint is designed.

## What activate all does to a build

Activating every tool is accepted, because the alternative is detecting which
tools a build uses before it has started. It means every enabled build gets:

| Tool | Written or changed |
|---|---|
| Gradle | `~/.gradle/init.d/bitrise-build-cache.init.gradle.kts`, a `gradle.properties` block |
| Bazel | a managed block in `~/.bazelrc`: credential helper, remote cache, BES streaming |
| Xcode (macOS) | `xcodebuild` and `xcrun` wrappers first on `PATH`, `~/.bitrise-xcelerate`, a background cache proxy |
| C++ | ccache install and config; the React Native setup |
| All | `BITRISE_BUILD_CACHE_*` and benchmark-phase exports through `envman` |

Measured cost on a macOS VM: about 6 seconds for all four.

## Validation so far

Everything below ran on the **staging DEN** (`worker.use_bitrise_den_staging`),
on macOS VMs, with a build of the `bitrise` CLI swapped in through the agent's
release file and the pin handed over by the preboot script. Test workflows are
copies of existing ones with the activate Step removed.

These runs used the previous shape of the hook, where the `bitrise` CLI looped
over the tools itself and used the released build cache CLI. The thin wrapper
calling `activate all --auto` is the same flow with the loop and the gate moved
into the CLI, and is being re-run on the same setup.

| Tool | Build | Result |
|---|---|---|
| Xcode, WordPress-iOS `trunk` on Xcode 27 | IAD, cache working, first run | 65% blob hits, 10.8 min `build-for-testing` |
| | IAD, after four runs | 99.9 to 100% blob and task hits, 4.0 min, 0 to 132 blobs uploaded |
| | ORD, third visit | 83.7% blob hits, 30.9% task hits, 8.1 min, still warming |
| Bazel, bazel at the e2e suite's pinned commit | AMS | success, 5.0 min, 179 of 5,161 actions from the remote cache, BES stream recorded; the datacenter's prior cache state is unknown |
| React Native iOS, Seek | IAD | the hook activated all tools; the build then failed at `pod install` because the staging Xcode 27 image lacks the Ruby the app pins. Not a cache failure, to be re-run |
| Gradle, DuckDuckGo | Linux | **not testable**, see below |

What the runs established:

- The hook installs from the host VM cache, activates every tool in about 6 s,
  and the first step already sees the exported `PATH` (the Xcode wrapper first).
- With the cache reachable, warm builds are fast and stable, but warming is not
  instant and not monotonic: IAD went 65%, 36%, 77%, 99.9% over about 90 minutes
  of runs, and ORD was still at 31% task hits after three visits. The cause of the
  slow warm-up is not established.
- A build the allowlist excludes writes nothing and prints one line. This is
  covered by unit tests and a local smoke test, and not yet by a staging build
  from an excluded organization.

Not validated:

- **Linux and containers.** Builds run `bitrise` inside `bitrise-main-container`.
  The container copies the agent's release file into place only when the image's
  `bitrise` differs from the version DEN asks for, and the image already carries
  the version DEN asks for, so a swapped-in binary is never used and the hook
  never runs. Linux can only be tested with a real `bitrise` release that DEN
  then requests, either globally or through its per-organization version override
  (read from the agent code, not exercised).
- The entitlement cases: trial, active subscription, none. They need the endpoint.
- An explicit activate Step in a workflow on an auto-activated VM.
- Builds that use none of the activated tools, and Tuist or other callers of
  `xcodebuild` by absolute path.
- Production DEN and non-macOS-arm64 VMs.

## Rollout plan

Organizations are enabled one at a time, first by a list hardwired in the preboot
script, later by the website endpoint. Each step limits what the next can break.

0. **Releases, once each.**
   - Build cache CLI: release once, after the `activate all` and gate change is
     merged. It is inert on its own: nothing runs `activate all --auto` unless
     the `bitrise` hook is enabled by env. The existing automation then bumps the
     preboot pin.
   - `bitrise` CLI: release once, after the thin wrapper is merged, so the hook
     ships a single time. The hook is inert unless `BITRISE_BUILD_CACHE_ACTIVATE_ALL`
     is set, so it changes nothing for anyone else, including self-hosted users.
   - Do not release either to try something. Staging runs use prerelease
     binaries that cannot trigger the pipeline: a tag outside the `vX.Y.Z[-pre]`
     pattern, published somewhere the pipeline does not watch. A `v`-prefixed
     semver prerelease tag does trigger it, and the pipeline chains into the
     automatic preboot bump.
1. **Staging preboot** (done in a test form): export the opt-in, the allowlist and
   the host cache URL, and hand over the pin. Validate every tool and OS on
   staging, including the negative case: a build from a workspace not on the list
   must activate nothing and report no invocation.
2. **Linux and containers.** Needs step 0's `bitrise` release and a DEN request
   for it. Until then Linux builds are not activated at all, because the container's
   `bitrise` has no hook.
3. **Production preboot for internal workspaces only.** Allowlist: the Bitrise
   monitoring and Advanced CI workspaces. Watch invocation counts, failures, and
   build duration.
4. **Opt in organizations one by one.** One preboot change per organization,
   adding its slug to the list. Before each: confirm the workspace has a trial or
   subscription, and tell support and sales that cache activity will appear for a
   workspace with no Step. After each: watch the same signals for a day.
   Rollback is removing the slug.
5. **Website endpoint ships.** Replace the list with the entitlement answer per
   tool: flip `entitlementEndpointShipped`, delete the bypass, and remove the
   allowlist. Opting in is then a product setting instead of a deploy.
6. **General availability.** Remove the opt-in env from preboot so the endpoint is
   the only gate.

Rollout and rollback are slower than the config change suggests. VMs are pooled
and boot ahead of demand. On staging, builds ran on VMs that had booted 8 to 19
hours before the latest sync, with the previous script. A slug added to the list
reaches a VM only when it boots, and a slug removed keeps activating on VMs that
booted before the removal. To stop a misbehaving organization immediately, the
allowlist has to be changed where the running build can see it, not only in the
boot script.

## Risks of activating everything

Roughly in order of how likely they are to matter.

1. **Analytics for builds that did not ask.** The allowlist is the only guard until
   the endpoint ships. A wrong or `*` entry reports for every workspace on the VM.
2. **Slow, stale rollback.** See above: pooled VMs keep the script they booted with.
3. **Tools the build does not use get configured.** A Gradle init script for a build
   that has no Gradle, a `~/.bazelrc` block, the Xcode wrappers on `PATH`. Most are
   harmless on an ephemeral VM; the Xcode wrappers change what every `xcodebuild`
   call does.
4. **Interaction with an explicit activate Step.** A workflow that already has a
   Step now activates twice. Both activations are meant to be idempotent, which has
   not been tested.
5. **The Xcode wrapper and proxy.** Builds that call `xcodebuild` or `xcrun` by
   absolute path bypass the wrapper and get no cache (Tuist is one known case). A
   background cache proxy is started for every Xcode-capable build, and an earlier
   daemon design made builds slower.
6. **Gradle compatibility.** The generated init script has to compile across the
   Gradle versions customers use. It already caused fleet-wide breakage once when it
   used an API whose nullability differs by version.
7. **Cold caches and egress.** The first builds in a datacenter find nothing and
   upload everything. A cold Xcode build uploaded 3.6 to 4.2 GB. Warm-up took
   several runs per datacenter on staging, so early adopters see little benefit and
   full upload cost.
8. **Time added to every build.** About 6 seconds when everything works. Up to two
   minutes if the cache backend or the install source is slow, because the hook
   waits for the limit.
9. **Silent non-activation.** A skipped or failed activation never fails the build.
   A customer who expects the cache sees none, with one log line as the only clue.
10. **Credential exposure surface.** The services token is handed to a child
    process. Only `BITRISE_BUILD_CACHE_*` and that token are passed, so the build's
    other secrets are not, and the CLI logs a hash instead of the token.
11. **Version skew.** Three components agree on an env contract: preboot, `bitrise`
    and the build cache CLI. A mismatch is a skipped activation, not a failure, so
    it is easy to miss. The pin handoff goes through a file in `/tmp` because the
    pin is set in a background subshell on macOS.
12. **Install source availability.** The host VM cache and GAR are both used for
    release tarballs. Both failing means no activation for that build.
13. **Outside CI.** Off Bitrise, the CLI's keychain write can hang until the
    two minute limit. The hook only runs when the env opt-in is set, which a laptop
    does not have.
14. **Staging does not match production.** Staging's preboot pinned an older cache
    cluster and broke both the cache and the Maven mirror for IAD and ORD until its
    IPs were synced. Validate on staging only after checking it still matches
    production.
15. **Entitlement is asked per tool.** Once the endpoint ships, `activate all` asks
    once for the set and each tool it then runs asks again, so a build makes up to
    five requests at five seconds each in the worst case. The answer should be
    cached for the build before that.

## Open questions

- Should the allowlist also be readable at build time from somewhere other than the
  boot script, so rollback does not wait for VMs to recycle?
- Does an explicit Step on an auto-activated VM double count invocations?
- Can the activated tool list be narrowed by what the build declares
  (`project_type`, the presence of `build.gradle`, an Xcode project) without
  becoming a second source of truth?
- Who owns asking DEN for the per-organization `bitrise` version, and in what order
  relative to the preboot change?
