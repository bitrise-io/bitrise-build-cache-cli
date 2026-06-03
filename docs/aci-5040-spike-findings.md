# ACI-5040 — Xcode.app ↔ Xcelerate Proxy Connection Lifecycle Spike

**Ticket:** [ACI-5040](https://bitrise.atlassian.net/browse/ACI-5040)
**Parent:** [ACI-5025 — M2 — Xcode.app + analytics](https://bitrise.atlassian.net/browse/ACI-5025)
**Date:** 2026-06-02 / 2026-06-03
**Status:** Findings — drives F1 / F2 / E2 design decisions in M2.

## Why this spike

The M2 milestone needs to ship local invocation analytics for Xcode.app GUI builds (problem P8 + P4). Before designing the daemon-side invocation correlation logic, we needed empirical answers to five questions about how Xcode.app talks to our xcelerate proxy:

1. Does Xcode open a connection per build, or hold one idle?
2. Multi-target build = N parallel connections, or one shared?
3. What does cancellation look like to the server?
4. How long until idle connections close?
5. Does index-while-building reach the remote cache socket?

## Apparatus

- **Project under test:** `Dimillian/IceCubesApp` (Mastodon client, 78-target dependency graph through SwiftPM).
- **Build host:** Xcode 26.5, SDK iPhoneSimulator 26.5, Apple Silicon.
- **Proxy:** `xcelerate start-proxy` from a spike branch with a `google.golang.org/grpc/stats.Handler` plugged in. Logs `TagConn` / `ConnBegin` / `ConnEnd` (with duration + RPC count) + `RPCBegin` / `RPCEnd` (with method, duration, error, ns timestamp). Gated by `BITRISE_BUILD_CACHE_SPIKE_STATS=1`.
- **xcconfig injection:** initially tried Apple's `XCODE_XCCONFIG_FILE` override slot via `launchctl setenv` — **did not propagate** to Xcode.app's running process. Switched to **project base xcconfig** (edited `IceCubesApp.xcconfig` in the test repo) which Xcode.app re-reads every build. Worked first try.
- **Drivers tested:** both `xcodebuild` CLI (Apple binary, bypassing the Bitrise wrapper-shim) and Xcode.app GUI.

## Findings

### Q1. Per-build vs idle-held

| Driver | Behavior |
|---|---|
| `xcodebuild` CLI | Connections open at build start, all close at build end. No idle hold (process exits). |
| **Xcode.app GUI** | **Connections held open after the build.** XCBBuildService is a resident, long-lived process and pools channels across builds. |

### Q2. Multi-target = N or 1?

| Driver | Connection count for the 78-target IceCubes build |
|---|---|
| `xcodebuild` CLI (`-j18`) | ~36 |
| Xcode.app GUI | ~18 |

Both are N, not 1. The number tracks build-system worker pool size, not target count. Xcode.app GUI pools more aggressively than the per-invocation CLI process.

### Q3. Cancellation

When a build is cancelled (we tested `xcodebuild` SIGINT + SIGKILL), the proxy sees:

- `code = Unavailable, desc = "transport is closing"` on every in-flight RPC.
- `ConnEnd` on all affected connections within sub-second.

This is gRPC's "client transport dropped" signature, distinct from a clean `err = ok` completion. Aborted builds are reliably distinguishable from clean ones from the server-side log alone.

### Q4. Idle close window

For Xcode.app GUI: after the build completes, channels stay open. After **~30 minutes** of no traffic, XCBBuildService closes them — Xcode itself stays running. Per-channel teardown is staggered over ~7 seconds (looks like per-channel client-side keepalive timeouts firing independently, not a single shutdown step).

### Q5. Index-while-building

**Zero RPCs.** Typing into a Swift file inside Xcode produced no traffic on the cache socket at all. Index-while-building uses Swift's incremental driver and local artifacts only — nothing goes through `COMPILATION_CACHE_REMOTE_SERVICE_PATH`. Only build / test / archive actions issue cache traffic.

### Bonus — back-to-back builds (correlation validation)

To confirm that closely-spaced builds can be distinguished by traffic patterns, ran two consecutive content-changed builds in the same Xcode session:

| Event | First RPC offset | Burst duration | RPCs |
|---|---|---|---|
| Build A | t = 232 s | ~1 s | 28 |
| Build B | t = 255 s | ~1 s | 28 |
| **Silence between A and B** | — | **22.2 s** | 0 |

Each burst is sub-second; the gap between them is 22 seconds. Trivially splittable with any silence-based time window of order seconds.

Important methodological note: plain `touch` (mtime-only) does **not** trigger cache traffic — Apple's CAS keys are content-addressed, so identical content hits the local plugin cache without ever reaching the remote socket. Force a real content change to get realistic cache traffic in a test.

## RPC traffic shape (representative single GUI build)

| Method | Share |
|---|---|
| `compilation_cache_service.cas.v1.CASDBService/Save` | 39 % |
| `compilation_cache_service.keyvalue.v1.KeyValueDB/GetValue` | 28 % |
| `compilation_cache_service.cas.v1.CASDBService/Load` | 22 % |
| `compilation_cache_service.keyvalue.v1.KeyValueDB/PutValue` | 11 % |

Service package matches our `proto/llvm/cas` + `proto/llvm/kv` definitions — no protocol surprises.

## Implications for downstream tickets

### F1 — Slim invocation emit on proxy session close ([ACI-5044](https://bitrise.atlassian.net/browse/ACI-5044))

**Design must be time-window based, not per-connection.**

- Per-connection correlation cannot work: a single build = 18–36 concurrent channels; channels are held open across builds in GUI mode (Q1, Q4).
- Use **silence-debounce time-windowing**:
  - Open candidate invocation when RPC rate jumps above idle baseline.
  - Stream hit / miss / byte counters into it.
  - Close + emit when ≥2 s of silence elapses on the proxy.
  - Cross-correlate with `xcactivitylog` timestamps (F2) for enrichment.
- Validated empirically: 22 s silence between back-to-back builds. Any debounce between 2 s and ~10 s works.
- The 30-min idle close (Q4) is **not** the closing signal; silence-debounce is. Q4 just means orphan invocations don't accumulate forever even if our debounce logic somehow misses a close.

Confidence: high for the common one-developer / one-Xcode case. Open: parallel Xcode sessions and `xcodebuild` CLI + Xcode.app GUI sharing one proxy — rare cases; can be handled with per-channel sender tagging if they prove problematic in practice.

### F2 — `xcactivitylog` watcher + enrichment re-PUT ([ACI-5045](https://bitrise.atlassian.net/browse/ACI-5045))

Xcode writes one `xcactivitylog` per logical build event. Its `timeStart` / `timeStop` range maps cleanly onto F1's silence-debounced traffic window. Re-PUT keyed on F1's `InvocationID`. Two-phase emit is straightforward.

### E2 — `xcode-app enable` xcconfig content ([ACI-5041](https://bitrise.atlassian.net/browse/ACI-5041))

Must write **all nine keys** the wrapper currently passes via `xcodebuild` CLI args (see `internal/xcelerate/xcodeargs/CacheArgs`):

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

The four `COMPILATION_CACHE_*` keys alone are not sufficient — without the five Swift / Clang master toggles, `swiftc` never receives `-cache-compile-job` and the cache path stays off. We discovered this the hard way when an early test build showed `0 hits / 1896 cacheable tasks` despite the cache settings being present in the resolved build settings dump.

## Global injection blocker for Xcode.app GUI (Xcode 26.5 / macOS 26)

After the main spike landed, took an extra pass at finding a Xcode.app GUI injection mechanism that's **persistent** and **doesn't require modifying the customer's repo**. Three paths tested, none work on this OS+Xcode combo:

| Mechanism | Result |
| --- | --- |
| `launchctl setenv XCODE_XCCONFIG_FILE` (Aqua-session env, Apple-documented) | `launchctl getenv` returns the value, but Xcode.app's build pipeline never sees it. Resolved swiftc invocations show no `-cache-compile-job` flag; `xcactivitylog` shows no xcconfig reference. |
| Direct env on the Xcode binary (`XCODE_XCCONFIG_FILE=… /Applications/Xcode.app/Contents/MacOS/Xcode &`) | Env reaches Xcode's process, but XCBBuildService (which actually drives the build) doesn't inherit. |
| `defaults write com.apple.dt.Xcode IDEBuildOperationCustomBuildSettings -dict …` (undocumented) | Defaults write succeeds and persists, but `xcodebuild -showBuildSettings` never lists the keys. Phantom on Xcode 26.5. |

**Working paths today:**

- **Project base xcconfig** committed in the customer's repo (modifies `.xcodeproj`).
- **`xcodebuild` CLI** with env set in the same shell (per-invocation).

### Implication for E2 — refined

The "one command on a customer's machine to enable Xcode.app cache globally" UX **cannot ship today** on Xcode 26.5 / macOS 26. The wrapper cannot edit every customer's `.xcodeproj`. Two viable options:

1. **Repo-level injection** — the M3 repo-controlled config flow (`.bitrise/build-cache.json` → CLI writes / amends the project's base xcconfig) can drive this. Customer commits the resulting xcconfig change once.
2. **Wait for Apple to ship a global-injection mechanism** in a future Xcode release.

### Bonus discovery — `COMPILATION_CACHE_ENABLE_CACHING` is the Xcode 26 master toggle

While hunting for working injection paths, found that Xcode 26 introduced a **new top-level toggle**: `COMPILATION_CACHE_ENABLE_CACHING = YES`. Without it, the individual `COMPILATION_CACHE_ENABLE_PLUGIN` + `SWIFT_ENABLE_COMPILE_CACHE` + `CLANG_ENABLE_COMPILE_CACHE` keys are insufficient on their own in some configurations.

Today our wrapper passes the 8 individual keys (from `internal/xcelerate/xcodeargs/CacheArgs`) but not this master toggle. **E2's xcconfig should include `COMPILATION_CACHE_ENABLE_CACHING = YES` as a 10th key.** Worth adding to `CacheArgs` for consistency between CLI and GUI paths.

## Loose ends / follow-ups

- **`ProxySocketPath` is read from `~/.bitrise-xcelerate/config.json` only** when starting the proxy. `BITRISE_XCELERATE_PROXY_SOCKET_PATH` is honored by `activate` (which writes the config) but not by `start-proxy`. Small fix; not blocking.
- **Scenarios not tested:** Xcode.app GUI test runs (⌘U vs ⌘B), Run action (build + launch), simultaneous parallel Xcode windows sharing one proxy. None of these change the F1/F2/E2 conclusions; can be handled at impl time.

## Repo references

- Technical spike doc + apparatus reproduction: `docs/xcode-app-proxy-lifecycle-spike-2026-06-02.md`
- Spike branch: `aci-5040-xcode-app-proxy-spike`
- Instrumentation: `internal/xcelerate/proxy/spike_stats_handler.go`
