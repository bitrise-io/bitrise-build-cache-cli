# Build-start activation (`activate all --auto`)

Ticket: ACI-5515. Getting Build Cache onto builds that never added a Step.

When a platform starts a build, the `bitrise` CLI installs the build cache CLI once
and runs `bitrise-build-cache activate all --auto` before the first workflow. At
that point the build's credential, identity and environment already exist, so
every tool gets the same full activation a Step would have run and nothing has to
be deferred to the build tool. The same install also runs the Gradle repository
mirrors, so the VM no longer installs the CLI a second time at boot.

## Architecture

Three components, each doing one job.

```
 preboot startup script            bitrise CLI (thin wrapper)           bitrise-build-cache
 ----------------------            --------------------------           -------------------
 pins CLI version + sha256   --->  installs that version once:          activate all --auto
 exports the opt-in envs,          host VM cache, else GAR,             1. resolve the credential
 the org allowlist and the         verified against the sha256          2. org gate (fail closed)
 host cache URL                    runs activate gradle-mirrors         3. entitlement (fail open)
                                   runs activate all --auto with the    4. activate each tool
                                   build's cache envs, hands back       hands back envman exports
                                   its envman exports
```

**Preboot** owns policy that is per VM: which CLI version, which organizations,
where the host cache is. It exports these into the DEN agent's environment, and
the agent already passes `BITRISE_*` variables through to `bitrise run`.

**The `bitrise` CLI** owns nothing about caching. In `bitrise run`, after the
build's envs are assembled and before the first workflow, `internal/buildcache`
does the following:

1. Skips a nested `bitrise run` (the step execution id is already set) and
   returns unless one of two independent opt-ins is `true`:
   `BITRISE_BUILD_CACHE_ACTIVATE_ALL` or
   `BITRISE_BUILD_CACHE_ACTIVATE_GRADLE_MIRRORS`. The mirrors are also skipped when
   `BITRISE_DEN_DISABLE_HOSTS_OVERRIDE=true`, as the boot script did.
2. Installs `bitrise-build-cache` once, at `BITRISE_BUILD_CACHE_CLI_VERSION`, checks
   the tarball against `BITRISE_BUILD_CACHE_CLI_SHA256`, and links it into
   `~/.bitrise/tools`, which is on the build's `PATH`. Source order is
   `BITRISE_BUILD_CACHE_CLI_HOST_CACHE_URL`, then the public GAR mirror.
3. With the mirrors opt-in, runs `bitrise-build-cache activate gradle-mirrors -d`
   with `BITRISE_MAVENCENTRAL_PROXY_ENABLED=true`. The generated init script
   re-reads that variable at Gradle build time, so a build can still switch the
   mirrors off.
4. With the activation opt-in, runs `bitrise-build-cache activate all --auto` with
   only the `BITRISE_BUILD_CACHE_*` envs, the services token and the Build Hub VM
   token and URL (`BITRISEIO_BUILD_HUB_VM_TOKEN`, `_URL`) from the build, so the CLI
   can broker a Build Cache token on a Build Hub runner. It hands the CLI's
   `envman` exports to the first step.

Each command runs under the same two minute limit, and one failing does not stop
the other. A failure at any point is a warning, never a failed build. There is no
version pin in the `bitrise` repo: preboot already pins the version and sha256 and
the release automation already bumps them.

**Why the mirrors moved.** They used to be activated at VM boot by a parallel phase
that downloaded the same CLI to `/tmp/bin`. Both installs hit the host cache, so
the second was pure waste. The mirrors are still configured before the first step,
but per build instead of per VM boot, and for every build instead of per
organization. The `/etc/hosts` pinning for the mirror hostnames stays in the boot
script, because hosts writers must not race.

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
   commas or whitespace, or `all` (`*` also works) to bypass the gate for every
   workspace. The workspace comes from the build's own
   credential (`BITRISE_BUILD_CACHE_WORKSPACE_ID`, else the `org_id` claim of the
   services token). No credential, no workspace, no list, or a workspace not on
   the list all mean: log one line, write nothing, exit 0. `all` skips the list but
   not the credential: with no workspace resolved, nothing is activated.
2. **Entitlement, fails open.** Entitlement is per workspace, not per build tool.
   Every `activate <tool>` command, and `activate all`, asks whether the workspace
   has Build Cache and stops before writing anything on an explicit "no", printing
   where to start a trial. The answer is three-valued: an unreachable website, a missing
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

The Gradle mirrors are a separate opt-in and are not part of `activate all`: they
write their own Gradle init script and are enabled for every build on the VM, not
per organization.

Measured cost on a macOS VM: about 6 seconds for all four.

## Validation so far

Everything below ran on the **staging DEN** (`worker.use_bitrise_den_staging`),
on macOS VMs, with a build of the `bitrise` CLI swapped in through the agent's
release file and the pin handed over by the preboot script. Test workflows are
copies of existing ones with the activate Step removed.

### Final stack (thin wrapper, `activate all --auto`, prerelease with the gates)

Bitrise CLI `3.1.0-aci5515` and build cache CLI `3.99.0-aci5515.2`, org allowlist
set in the preboot script. Activation took about 1 s in every build.

| Tool | Result |
|---|---|
| Xcode, WordPress-iOS `trunk` | success, 4280 of 4280 task hits (100%), invocation saved |
| Bazel, e2e suite's pinned commit | success, 4449 of 5161 processes from the remote cache, `bazel build` 30.6 s |
| Gradle on macOS, e2e workflow | success in 2m 9s, 40 tasks from cache against 17 executed, 4036 tasks uploaded to analytics |
| Negative control (Xcode, allowlist overridden to a different org) | `auto-activation skipped: workspace "<id>" is not enabled for it`, no invocation, build unaffected |
| React Native iOS, Seek | activated, pods installed, wrapper saved the RN and Xcode invocations; the app's own Xcode build then failed (pods target iOS 9.0, Xcode 27 supports 15.0 and up). Not a cache failure |

### Earlier runs (previous hook shape)

The `bitrise` CLI looped over the tools itself and used the released build cache
CLI. Same flow, with the loop and the gate since moved into the CLI.

| Tool | Build | Result |
|---|---|---|
| Xcode, WordPress-iOS `trunk` on Xcode 27 | IAD, cache working, first run | 65% blob hits, 10.8 min `build-for-testing` |
| | IAD, after four runs | 99.9 to 100% blob and task hits, 4.0 min, 0 to 132 blobs uploaded |
| | ORD, third visit | 83.7% blob hits, 30.9% task hits, 8.1 min, still warming |
| Bazel | AMS | success, 5.0 min, 179 of 5,161 actions from the remote cache (cold datacenter) |
| Gradle, DuckDuckGo | Linux | **not testable**, see below |

### What the runs established

- The hook installs from the host VM cache, activates every tool in about 1 to 6 s,
  and the first step already sees the exported `PATH` (the Xcode wrapper first).
- The allowlist gate holds: an excluded organization writes nothing, prints one
  line and sends no analytics (staging negative control).
- With the cache reachable, warm builds are fast and stable, but warming is not
  instant and not monotonic: IAD went 65%, 36%, 77%, 99.9% over about 90 minutes
  of runs, and ORD was still at 31% task hits after three visits. The cause of the
  slow warm-up is not established.

### Test setup gotchas

- Staging VMs pool and keep the startup script they booted with, so a script change
  only reaches VMs booted after the deploy.
- Staging's `/etc/hosts` cache IPs went stale and caused TLS handshake EOFs and
  Maven mirror failures until they were synced from production.
- The edge Mac image only has Xcode 27, which breaks older apps (private headers,
  `-Wl,-print_statistics`, Swift 6 data-race errors, pods below iOS 15).
- The build log service answers 429 on log floods: redirect `xcodebuild` output to a
  file and upload it as an artifact.
- A build's own `BITRISE_BUILD_CACHE_*` envs override the preboot allowlist, so the
  negative control is just an env override on the trigger.
- Prereleases for the preboot script must not use a tag the release automation matches.

Not validated:

- **Linux and containers.** Builds run `bitrise` inside `bitrise-main-container`.
  The container copies the agent's release file into place only when the image's
  `bitrise` differs from the version DEN asks for, and the image already carries
  the version DEN asks for, so a swapped-in binary is never used and the hook
  never runs. Linux can only be tested with a real `bitrise` release that DEN
  then requests, either globally or through its per-organization version override
  (read from the agent code, not exercised).
- The Gradle mirrors from `bitrise run` with the boot-time phase removed: the hook
  is unit tested and a staging run is pending. Linux keeps its boot-time phase.
- The `all` allowlist value and per-workspace entitlement: unit tested, not yet
  run on staging (the staging prerelease predates both).
- The entitlement cases: trial, active subscription, none. They need the endpoint.
- An explicit activate Step in a workflow on an auto-activated VM.
- Builds that use none of the activated tools, and Tuist or other callers of
  `xcodebuild` by absolute path.
- React Native iOS cache hits (the Seek build needs a deployment-target patch on Xcode 27).
- Production DEN and non-macOS-arm64 VMs.

## Observability

The mirror activation used to log from the VM startup script, where the
allocation analytics router classified two signals: any line starting with
`activating bitrise-build-cache gradle-mirrors failed:` (a failure) and the host
cache fallback line (a trend, not a failure). Once `bitrise run` does the
activation those lines are written to the **build log**, so they move from
allocation analytics to build-log analytics:

- Build-log classification rules for the same two signals, in
  `build-analytics-deployments` (`staging` branch, which covers production). The
  allocation rules stay while the Linux boot-time phase still emits the lines.
- Datadog monitors on the new signals in `internal-platform-services`: the mirror
  activation failure rate and the host-cache fallback trend.
- The strings are part of the contract between the `bitrise` CLI and the
  classifier, and have to stay stable across rewordings.

These must be live before the boot-time phase is removed from production
preboot, or the failure signal goes dark for that window.

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
   must activate nothing and report no invocation. Also validate the Gradle
   mirrors from `bitrise run` with the boot-time phase removed: the init script
   must come from the build log, and the CLI must be installed once.
2. **Observability before the mirrors move.** The build-log classifier rules and
   the Datadog monitors above go live first.
3. **Linux and containers.** Needs step 0's `bitrise` release and a DEN request
   for it. Until then Linux builds are not activated at all, because the container's
   `bitrise` has no hook.
4. **Production preboot, one PR.** Pass the pin and host cache URL, opt in the
   internal workspaces (Bitrise monitoring and Advanced CI) and the Gradle
   mirrors, and drop the boot-time mirror phase. **Merge it only after a `bitrise`
   release with the hook is what DEN requests on production macOS VMs.** Until
   then no build runs the mirrors, so merging earlier drops them fleet-wide.
   Running both is harmless, since the init script is identical. Watch invocation
   counts, failures, mirror activation failures and build duration.
5. **Opt in organizations one by one.** One preboot change per organization,
   adding its slug to the list. Before each: confirm the workspace has a trial or
   subscription, and tell support and sales that cache activity will appear for a
   workspace with no Step. After each: watch the same signals for a day.
   Rollback is removing the slug.
6. **Website endpoint ships.** Replace the list with the entitlement answer per
   workspace: flip `entitlementEndpointShipped`, delete the bypass, and remove the
   allowlist. Opting in is then a product setting instead of a deploy.
7. **General availability.** Remove the opt-in env from preboot so the endpoint is
   the only gate. `BITRISE_BUILD_CACHE_AUTO_ACTIVATE_ORGS=all` is the interim way to
   bypass the allowlist for every workspace without removing the gate code.

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
   the endpoint ships. A wrong or `all` entry reports for every workspace on the VM.
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
15. **Entitlement is asked once per command.** It is per workspace, but `activate all`
    asks and each tool it then runs asks again, so once the endpoint ships a build
    makes up to five requests at five seconds each in the worst case. The answer
    should be cached for the build before that.
16. **The mirrors move from boot to build start.** They are configured per build,
    for every build, and the failure signal moves from the VM log to the build log.
    A slow or failing install now adds to every build's start instead of the VM's
    boot, and the classifier and monitors must be live first (see Observability).

## Open questions

- Should the allowlist also be readable at build time from somewhere other than the
  boot script, so rollback does not wait for VMs to recycle?
- Does an explicit Step on an auto-activated VM double count invocations?
- Can the activated tool list be narrowed by what the build declares
  (`project_type`, the presence of `build.gradle`, an Xcode project) without
  becoming a second source of truth?
- Who owns asking DEN for the per-organization `bitrise` version, and in what order
  relative to the preboot change?
