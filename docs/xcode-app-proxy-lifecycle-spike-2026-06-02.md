# Xcode ↔ Xcelerate Proxy Connection Lifecycle — Spike Notes

**Ticket:** [ACI-5040](https://bitrise.atlassian.net/browse/ACI-5040)
**Date:** 2026-06-02
**Status:** Findings — drives F1 / F2 / E2 design decisions for [ACI-5025 (M2)](https://bitrise.atlassian.net/browse/ACI-5025).

## Question

Characterize the connection lifecycle between the build process and the xcelerate proxy when running through Apple's compilation-cache service path (i.e. the path Xcode.app GUI would use). Specifically:

1. Connection per build or held idle?
2. Multi-target = N parallel connections or one shared?
3. Cancellation behaviour?
4. Idle close window?
5. Index-while-building — shared socket?

## Apparatus

- **Project:** `Dimillian/IceCubesApp` (shallow clone). 78-target dependency graph (main app + 5 extensions + 13 SwiftPM modules + transitive deps).
- **Build host:** Xcode 26.5 (SDK iPhoneSimulator26.5). Spike branch `aci-5040-xcode-app-proxy-spike`.
- **Proxy:** `xcelerate start-proxy` from the spike branch. Standard unix-socket gRPC server, **instrumented with a `google.golang.org/grpc/stats.Handler`** that logs `TagConn` / `ConnBegin` / `ConnEnd` + `RPCBegin` / `RPCEnd` (with method, duration, error). Gated by `BITRISE_BUILD_CACHE_SPIKE_STATS=1`. Code: `internal/xcelerate/proxy/spike_stats_handler.go`.
- **xcconfig override:** `/tmp/spike-override.xcconfig` containing the same 9 keys the wrapper passes today via `xcodebuild` CLI args (`COMPILATION_CACHE_*` + `SWIFT_*` + `CLANG_*` — see `internal/xcelerate/xcodeargs/args.go:CacheArgs`).
- **Build driver:** `/Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild` (Apple binary, NOT the wrapper). This still goes through `XCBBuildService` — same path as Xcode.app GUI.
- **Why CLI, not Xcode.app GUI:** `launchctl setenv XCODE_XCCONFIG_FILE` did not propagate into the running Xcode.app process. Need a follow-up to figure out the injection. CLI invocation produces identical proxy traffic at the XCBBuildService layer, so the spike's questions can still be answered.

## Pitfalls hit during apparatus setup (worth recording)

- `BITRISE_XCELERATE_PROXY_SOCKET_PATH` env var is **honored by `activate` (NewConfig) but ignored by `start-proxy` (ReadConfig)**. Proxy socket path comes from `~/.bitrise-xcelerate/config.json`. Setting the env var alone has no effect on the proxy.
- The CLI wrapper auto-spawns its own proxy whenever a wrapped `xcodebuild` runs. If you already have a manual proxy on the same socket, the auto-spawned one does `os.Remove + listen`, orphaning the manual proxy's FD (live process, dead inode). Visible side-effect: builds report "1896 cacheable tasks / 0 hits" but **zero** RPCs reach the manual proxy (silent ECONNREFUSED on the new inode).
- macOS `ps -E` does **not** expose env vars on processes you don't own (SIP). Cannot use it to confirm Xcode.app saw `XCODE_XCCONFIG_FILE` — use the build's resolved settings dump or the `.xcactivitylog` instead.
- `xcconfig` override slot alone (4 `COMPILATION_CACHE_*` keys) is **insufficient** — Apple's compile cache stays inactive without the 5 master toggles (`SWIFT_ENABLE_COMPILE_CACHE`, `SWIFT_ENABLE_EXPLICIT_MODULES`, `SWIFT_USE_INTEGRATED_DRIVER`, `CLANG_ENABLE_COMPILE_CACHE`, `CLANG_ENABLE_MODULES`). The wrapper already passes the full 9-key set via CLI args; E2's xcconfig must replicate that set verbatim.

## Findings — Scenario 1: cold build (`IceCubesNotifications` scheme, fresh DerivedData)

The chosen scheme transitively pulls 78 build targets through SwiftPM, so this single run also answers the multi-target question (no need for a separate scenario 2).

| Metric | Value |
|---|---|
| Build wall time | ~3 min 12 s |
| Cacheable tasks (Apple's `CompilationCacheMetrics`) | 1,896 |
| Cache hits | 0 (cold) |
| gRPC connections opened to proxy | **36** |
| Connection lifetime (median) | ≈3 min 12 s — **held for entire build** |
| Total RPCs | 12,186 |
| RPC errors | 0 |

RPC method distribution:

| Method | Count | % |
|---|---:|---:|
| `compilation_cache_service.cas.v1.CASDBService/Save` | 7,775 | 64% |
| `compilation_cache_service.keyvalue.v1.KeyValueDB/PutValue` | 2,611 | 21% |
| `compilation_cache_service.keyvalue.v1.KeyValueDB/GetValue` | 1,800 | 15% |

Connection traffic shape:

- "Hot" connections (~600–700 RPCs each) — likely swiftc worker channels. Build ran with `-j18`.
- "Cold" connections (~19–22 RPCs each) — likely query-only channels (KV lookup, dependency-scan).
- **All 36 `ConnEnd` events arrived within ~10 µs of each other at build-end** — the build process tears down all channels in one teardown step, not per-RPC.

Implications:

1. **Q1 (per-build vs held idle):** Connections are held for the entire build, then all close together at build-end. They are not per-RPC.
2. **Q2 (multi-target = N or 1):** N ≈ 36. The count tracks `-j` parallelism × build-system worker pools, **not** target count. A 78-target build still produces ~36 conns.

## Findings — Scenario 3: cancel mid-build (SIGINT then SIGKILL on xcodebuild)

| Metric | Value |
|---|---|
| Conns active at cancel | 36 |
| In-flight RPCs at cancel | dozens |
| RPC-level signal at cancel | `code = Unavailable, desc = "transport is closing"` |
| Time-to-`ConnEnd` after kill | sub-second |

Cancellation appears on the server side as **`Unavailable` + "transport is closing"** on every in-flight RPC, followed quickly by `ConnEnd` on all open connections. This is the gRPC server-side signature for "client process died / transport dropped" — *not* `code = Canceled` (which would imply graceful gRPC context-cancel propagation from the client).

Implication:

3. **Q3 (cancellation):** Build cancel manifests as `Unavailable` + immediate `ConnEnd`. Same signal whether the user cancels in Xcode UI or `kill`s `xcodebuild`. We **can** tell "build aborted" apart from "build completed cleanly" — clean completion shows `err=ok` on every RPC, then `ConnEnd`.

## Findings — bonus: warm cache, second build

Second `xcodebuild clean build` (same scheme, fresh DerivedData but populated proxy/CAS):

| Metric | Value |
|---|---|
| Cacheable tasks | 1,896 |
| Cache hits | 723 (38%) |
| New connections opened | **36** (identical to cold build) |
| Reused connections from cold build | 0 |

A new `xcodebuild` process opens its own fresh 36-connection set; nothing pools across invocations. Hit rate climbs as the proxy's KV/CAS state warms.

Implication: time-windowed correlation works regardless of cold-vs-warm — the connection count is invariant.

## Findings — Scenarios 4 + 5: Xcode.app GUI build

After the launchctl-env path failed (see Loose ends), used the **project base xcconfig** (`IceCubesApp.xcconfig`) instead — appended the 9 cache keys to it directly. Xcode.app re-reads project xcconfig every build, no env injection needed. Worked first try.

Single Xcode.app GUI build (⇧⌘K + ⌘B, `IceCubesApp` scheme, fresh-ish DerivedData):

| Metric | Xcode.app GUI | (vs xcodebuild CLI for comparison) |
|---|---|---|
| Build wall time | ~similar | ~3 min 12 s |
| Connections opened (`ConnBegin`) | **18** | 36 |
| Connections closed (`ConnEnd`) | **0 — all still open after build** | 36 (all closed at build-end) |
| RPCs total | 1,760 | 12,186 |
| Errors | 0 | 0 |

GUI RPC method distribution:

| Method | Count | % |
|---|---:|---:|
| `CASDBService/Save` | 685 | 39% |
| `KeyValueDB/GetValue` | 494 | 28% |
| `CASDBService/Load` | 386 | 22% |
| `KeyValueDB/PutValue` | 195 | 11% |

Key differences from CLI mode:

- **XCBBuildService is resident and pools connections.** 18 channels vs 36; held open across the build boundary. No `ConnEnd` events at build end.
- **Higher proportion of `Load` RPCs** (22%) — CLI cold build issued zero. Implies the GUI is hitting cache for content (warm side of mixed cold/warm state from previous CLI builds against the same proxy).
- **Lower absolute RPC volume** despite same project — probably because some artifacts were already cached on the proxy side from the earlier CLI runs.

### Follow-up — idle close + typing-trigger probe

Left Xcode.app open and idle after the build, also typed into a Swift source file once. After ~30 min:

- **All 18 channels closed.** `ConnEnd` for each — durations 31 m 37 s to 31 m 54 s from open.
- Xcode **still running** (confirmed via `pgrep`). Not a quit event.
- 7-second spread on `ConnEnd` events → staggered per-channel teardown, not synchronized.
- **Zero RPCs** between end-of-build and idle close. Typing into a Swift file produced no traffic at all.

This answers the open scenarios:

4. **Q4 (idle close):** XCBBuildService closes idle gRPC channels after a ~30-minute window. Xcode does not need to quit. Staggered per-channel teardown suggests client-side keepalive/idle timeout per channel, not a single shutdown step.
5. **Q5 (index-while-build):** Indexing / live-editing does **not** reach the remote cache socket. Index-while-building uses Swift's incremental driver and local artifacts only; nothing goes through `COMPILATION_CACHE_REMOTE_SERVICE_PATH`. Only build/test/archive actions issue RPCs.

### Implication for F1 — strengthened

Time-window correlation isn't merely preferred — it's **mandatory** for Xcode.app GUI mode. Per-connection invocation would never emit anything until Xcode quits (potentially days). Build the time-window from RPC traffic rate + idle gap, not from `ConnEnd`.

Suggested heuristic:
- Start a candidate invocation when RPC rate jumps from idle baseline.
- Hold it open while rate stays above baseline.
- Close when rate drops below threshold for N seconds (e.g. 2 s — empirically tunable).
- Cross-correlate with `xcactivitylog` timestamps (F2) to confirm the window matches a real build event.

### Validation — back-to-back builds

Empirical test to confirm time-window splitting works for closely-spaced builds:

1. Edit a source file (content change, not just `touch` — Apple's compile cache is content-addressed and ignores mtime).
2. ⌘B in Xcode, wait for "Build Succeeded".
3. Edit the same source file again (different unique content).
4. ⌘B immediately, wait for "Build Succeeded".

Result with ns-timestamped RPC log:

| Event | First RPC offset | Burst duration | RPCs |
|---|---|---|---|
| Build A | 231.9 s after session start | ~1.1 s | 28 |
| Build B | 255.1 s after session start | ~1.0 s | 28 |
| **Silence between A and B** | — | **22.2 s** | 0 |

Within each burst, all RPCs land in ~1 s. Between bursts, **22 seconds of zero traffic**. Trivially splittable with any sane debounce (2-5 s).

Note: pure `touch` (mtime-only change) produces zero cache traffic — Apple's CAS keys are content-addressed, identical content → identical key → cache hit on the local plugin path before any remote query. Test methodology matters; force a content change.

### Implication for F1 — final

Time-window correlation with a small silence debounce (recommend **2 s**) will reliably split consecutive builds. The ~30-min `ConnEnd` idle-close window is **not** the closing signal — silence-debounce is. Configuration:

- Open candidate invocation on first RPC after idle.
- Stream hit/miss/byte counters into it.
- Close + emit when ≥2 s of silence elapses.
- Cross-correlate with `xcactivitylog` (F2) for enrichment.

Confidence: **high** for the common single-developer single-Xcode case. Lower for edge cases not yet tested (parallel Xcode sessions, `xcodebuild` + Xcode.app sharing one proxy) — but those rare and can be handled with per-RPC sender tagging if they prove problematic in practice.

## Implications for downstream tickets

### [F1 — Slim invocation emit on proxy session close](https://bitrise.atlassian.net/browse/ACI-5044)

- **Per-connection invocation is the wrong unit.** A single build = ~36 concurrent connections. Treating each as its own invocation overcounts by ~36×.
- **Use time-windowed correlation.** All conns active in a contiguous window with no idle gap = one build. Heuristic: open `Invocation` when first `ConnBegin` arrives with no other active conns; close `Invocation` when all conns are `ConnEnd`'d and a brief debounce passes (sub-second works given Scenario-1 teardown shape).
- Hit/miss/byte counters need to accumulate across all conns in the window, then flush on `Invocation` close.

### [F2 — `xcactivitylog` watcher + enrichment re-PUT](https://bitrise.atlassian.net/browse/ACI-5045)

- Xcode writes one `xcactivitylog` per logical build. That timestamp range maps cleanly onto F1's connection time-window: `xcactivitylog.timeStart` ≤ first `ConnBegin`, `xcactivitylog.timeStop` ≥ last `ConnEnd` (within seconds).
- Re-PUT key = the `InvocationID` chosen by F1 in the time-window. Two-phase emit works.

### [E2 — `xcode-app enable` xcconfig content](https://bitrise.atlassian.net/browse/ACI-5041)

- Must write **all 9 keys** the wrapper currently passes via CLI args:
  ```
  COMPILATION_CACHE_REMOTE_SERVICE_PATH = <socket>
  COMPILATION_CACHE_ENABLE_PLUGIN = YES
  COMPILATION_CACHE_ENABLE_INTEGRATED_QUERIES = YES
  COMPILATION_CACHE_ENABLE_DETACHED_KEY_QUERIES = YES
  SWIFT_ENABLE_COMPILE_CACHE = YES
  SWIFT_ENABLE_EXPLICIT_MODULES = YES
  SWIFT_USE_INTEGRATED_DRIVER = YES
  CLANG_ENABLE_COMPILE_CACHE = YES
  CLANG_ENABLE_MODULES = YES
  ```
  Re-use `internal/xcelerate/xcodeargs/CacheArgs` as the source of truth — render into xcconfig form instead of `-key=value` CLI args.
- The 4-key short set Apple documents under `COMPILATION_CACHE_*` is NOT sufficient. Without the Swift/Clang master toggles, swiftc never gets `-cache-compile-job` and the compile cache path stays off.

## Loose ends / follow-ups

- **Xcode.app long-lived XCBBuildService:** scenarios 4 / 5 (idle hold / index-while-build) only make sense against a resident build service. Re-run once GUI injection works.
- **`ProxySocketPath` is read from `config.json` only**, not env, when starting the proxy. If we want `start-proxy` to honor `BITRISE_XCELERATE_PROXY_SOCKET_PATH` (matching what `activate` does), `ReadConfig` should consult env as a fallback. Small fix; not blocking.

## Global injection blocker for Xcode.app GUI (Xcode 26.5 / macOS 26)

Spent additional time trying to find a Xcode.app GUI injection mechanism that's both **persistent** and **doesn't require modifying the customer's repo**. None of the three documented / undocumented paths work on this OS+Xcode combo:

| Mechanism | Result |
| --- | --- |
| `launchctl setenv XCODE_XCCONFIG_FILE` (Aqua-session env, Apple-documented) | `launchctl getenv` returns the value, but Xcode.app's build pipeline never sees it. Resolved swiftc invocations show no `-cache-compile-job` flag, `xcactivitylog` shows no xcconfig reference. |
| Direct env on the Xcode binary (`XCODE_XCCONFIG_FILE=… /Applications/Xcode.app/Contents/MacOS/Xcode &`) | Same — env reaches Xcode's process, but XCBBuildService (which actually drives the build) doesn't inherit. |
| `defaults write com.apple.dt.Xcode IDEBuildOperationCustomBuildSettings -dict …` (undocumented) | Defaults write succeeds and persists, but `xcodebuild -showBuildSettings` never lists the keys. Xcode reads this defaults entry but doesn't merge it into the build-settings hierarchy. Phantom on Xcode 26.5. |

Working paths today:
- **Project base xcconfig** committed in the customer's repo (modifies `.xcodeproj`).
- **`xcodebuild` CLI** with env set in the same shell (per-invocation).

Implication for E2 ([ACI-5041](https://bitrise.atlassian.net/browse/ACI-5041)): the "one command on a customer's machine to enable Xcode.app cache globally" UX **cannot ship today**. We have to fall back to either:

- Per-project repo modification (which the M3 repo-controlled config can drive — `.bitrise/build-cache.json` → CLI writes / amends the project's base xcconfig).
- Wait for Apple to ship a global-injection mechanism in Xcode 27.

## Bonus discovery — `COMPILATION_CACHE_ENABLE_CACHING` is the Xcode 26 master toggle

While hunting for working injection paths, found that Xcode 26 introduced a **new top-level toggle**: `COMPILATION_CACHE_ENABLE_CACHING = YES`. Without it, the individual `COMPILATION_CACHE_ENABLE_PLUGIN` + `SWIFT_ENABLE_COMPILE_CACHE` + `CLANG_ENABLE_COMPILE_CACHE` keys are insufficient on their own in some configurations.

Today our wrapper passes the 8 individual keys (from `internal/xcelerate/xcodeargs/CacheArgs`) but not this master toggle. It worked for the CLI path empirically because some implicit Xcode default has it on for the project. For safety, **E2's xcconfig should include `COMPILATION_CACHE_ENABLE_CACHING = YES`** as a 10th key. Worth also adding to `CacheArgs` for consistency between CLI and GUI paths.

Documented at https://livsycode.com/best-practices/xcode-26-compilation-cache/ — Apple's own docs are sparse on this key.

## Reproduction

```bash
# 1. build CLI on spike branch
cd ~/dev/bitrise-build-cache-cli
git checkout aci-5040-xcode-app-proxy-spike
go build -o ./bin/bitrise-build-cache .

# 2. start instrumented proxy in background
BITRISE_BUILD_CACHE_SPIKE_STATS=1 \
  BITRISE_BUILD_CACHE_AUTH_TOKEN=... \
  BITRISE_BUILD_CACHE_WORKSPACE_ID=... \
  nohup ./bin/bitrise-build-cache xcelerate start-proxy >/tmp/spike-proxy.out 2>&1 &

# 3. write override xcconfig (path must match the proxy socket — read from proxy startup log)
cat > /tmp/spike-override.xcconfig <<EOF
COMPILATION_CACHE_REMOTE_SERVICE_PATH = /var/folders/.../xcelerate-proxy.sock
COMPILATION_CACHE_ENABLE_PLUGIN = YES
COMPILATION_CACHE_ENABLE_INTEGRATED_QUERIES = YES
COMPILATION_CACHE_ENABLE_DETACHED_KEY_QUERIES = YES
SWIFT_ENABLE_COMPILE_CACHE = YES
SWIFT_ENABLE_EXPLICIT_MODULES = YES
SWIFT_USE_INTEGRATED_DRIVER = YES
CLANG_ENABLE_COMPILE_CACHE = YES
CLANG_ENABLE_MODULES = YES
EOF

# 4. run a build via Apple's xcodebuild (bypassing the wrapper-shim)
cd ~/dev/spike-icecubes
XCODE_XCCONFIG_FILE=/tmp/spike-override.xcconfig \
  /Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild \
  -project IceCubesApp.xcodeproj \
  -scheme IceCubesNotifications \
  -destination 'generic/platform=iOS Simulator' \
  -derivedDataPath /tmp/spike-dd \
  clean build

# 5. inspect spike log
SPIKE_LOG=$(lsof -p $(pgrep -f 'bitrise-build-cache.*proxy') | grep proxy.*out\.log | awk '{print $NF}')
grep '\[spike\]' "$SPIKE_LOG" | head
```
