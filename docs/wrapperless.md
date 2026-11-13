# Wrapperless Xcode CAS pipeline

## Overview

The wrapperless flow attributes Xcode CAS traffic and build analytics to one BE invocation row per build **without** the xcodebuild wrapper injecting an `InvocationID`. The CLI watches DerivedData for filesystem events, classifies each build directly from `build-request.json` + `LogStoreManifest.plist`, and emits a two-phase row (slim on Start, enriched on End) under a deterministic InvocationID derived from the proxy identity and the manifest / xcbuilddata hex.

Opt-in flag on the proxy:

```
bitrise-build-cache xcelerate start-proxy --wrapperless-pipeline-enabled
```

It runs **alongside** the existing enricher — not as a replacement — so both flows produce rows and can be compared during beta testing.

---

## Why wrapperless

The wrapper flow rewrites the `xcodebuild` argv to inject an `-InvocationID` and telemetry env. Two problems it is designed to sidestep:

1. **IDE builds**: Xcode.app's ⌘B does not go through our wrapper, so the wrapper flow cannot stamp an InvocationID on IDE CAS traffic.
2. **CLI scripts that do not use the wrapper**: wrapper-less `/usr/bin/xcodebuild` calls lose attribution.

The wrapperless pipeline listens at the filesystem layer where every Xcode build (IDE and CLI) lands sealed artefacts, so neither surface is lost.

---

## Components

```
   ┌──────────────────────┐             ┌──────────────────────┐
   │ DDWatcher (fsnotify) │             │ QuitWatcher (kqueue) │
   │ ~/Library/.../        │             │ peer process exit    │
   │ DerivedData          │             │                      │
   └──────────┬───────────┘             └──────────┬───────────┘
              │ basename-dispatched                │
              │ handleEvent                        │
     ┌────────┴─────────┐                          │
     │                  │                          │
     ▼                  ▼                          ▼
  xcbuilddata     LogStoreManifest             process exit
  CREATE          WRITE                        event
     │                  │                          │
     ▼                  ▼                          ▼
  Start{hex,      End{uuid, scheme,            Quit{pid,
        action}         duration, status,             ancestry}
                        xcactivitylog}
     │                  │                          │
     └──────────┬───────┴──────────────┬───────────┘
                │                      │
                ▼                      ▼
           ┌───────────────────────────────┐
           │           Allocator           │
           │   single intake channel +     │
           │   bucket state machine        │
           └──────────────┬────────────────┘
                          │
            OnOpenInvoc + OnReady callbacks
                          │
                          ▼
                    ┌────────────┐
                    │  Emitter   │ ─→ BE PutInvocation (slim, then enriched)
                    └────────────┘
```

### DDWatcher — `internal/xcelerate/wrapperless/dd_watcher.go`

Single DerivedData watcher that merged the former StartWatcher + EndWatcher. One fsnotify source, one goroutine, one seed walk, one handled-UUID ledger.

Dispatches by basename:

| basename | event(s) | emits |
|---|---|---|
| `build-request.json` inside `*.xcbuilddata` | CREATE or WRITE | `Start{BuildRequestHex, Action}` via `handleXCBuildData` |
| `LogStoreManifest.plist` | CREATE or WRITE | `End{UniqueIdentifier, Scheme, Duration, Success, XCActivityLogPath}` via `scanManifest` |

(The seed walk also surfaces pre-existing `*.xcbuilddata` dirs and `LogStoreManifest.plist` files once at startup, so a proxy that joins mid-build still gets a chance to emit both signals.)

The `xcactivitylog` sibling path is read from the manifest entry's `fileName` field; falls back to `<UniqueIdentifier>.xcactivitylog` when absent.

Pre-start stale-skip: entries whose `TimeStopped` (CoreFoundation absolute time, seconds since 2001-01-01 UTC) precedes `w.startedAt - seedStaleGrace` are persisted to the handled store and skipped — guards against first-build-after-opt-in replay of old sealed entries.

### Allocator — `internal/xcelerate/wrapperless/allocator.go`

The state machine. Owns one open bucket at a time plus a speculative spec bucket that holds CAS traffic between builds.

Bucket reasons:

| Reason | Trigger | Row label |
|---|---|---|
| `BucketReasonEndMatched` | real Start + real End | `<action> [<scheme> / <configuration>]` |
| `BucketReasonEndOfPrior` | second concurrent Start evicts prior | same as above |
| `BucketReasonIDEBackground` | Quit from IDE-ancestry peer, speculative spec had CAS traffic | `Xcode IDE background` |
| `BucketReasonCLIImplicit` | Quit from CLI-ancestry peer, no matching End | `cli run` |
| `BucketReasonSpeculative` | between builds, no label — never emitted standalone | — |

Timeline events (Start / End / Quit) arrive on an in-process channel (multi-producer, single-consumer, overflow-drop at 10 000 events). CloseIntake signals Run to flush buffered events and return; the data channel is intentionally never closed to avoid the send-on-closed race against producers already past their intakeDone check.

**CLIImplicit suppression**: after a scheme-bearing End emits, the Allocator sets `schemeEmittedSinceStart` and `flushCLI` suppresses the fallback CLIImplicit bucket for the CAS teardown tail that lands on the post-End spec. The flag resets on the next Start.

### Emitter — `internal/xcelerate/wrapperless/emitter.go`

Two-phase BE PUT with HTTP-outboard replay. Successful PUTs land on the BE's Invocation API; failures persist to `~/.local/state/xcelerate/wrapperless/pending-{slim,enriched}.ndjson` and are retried by a background goroutine at 30 s cadence. Entries older than 30 d are dropped.

Row rendering uses the shared `internal/xcelerate/shortcmd` renderer:

```go
shortcmd.Render(action, scheme, testPlan, configuration)
// → "build [MyScheme / MyPlan / Debug]"
```

Both the wrapper flow (`xcodeargs.Default.ShortCommand`) and the wrapperless emitter consume it, so the BE row format cannot drift between the two flows. Wrapperless passes empty `testPlan` — Xcode's `LogStoreManifest.plist` signature does not carry it; parsing from `xcactivitylog` is a separate follow-up.

### HandledManifestStore — `internal/xcelerate/wrapperless/handled_manifests.go`

NDJSON ledger at `~/.local/state/xcelerate/wrapperless/handled-manifests.ndjson`. Closes the dup-row race across proxy restarts: a restart within `seedStaleGrace` of a sealed manifest would otherwise re-mint under a different InvocationID (anchor differs because ProxyPID differs).

`LoadFreshAndCompact` reads and compacts in one pass at hydrate: records within `HandledManifestMaxAge` (7 d) hydrate into the in-memory seen set; stale records are dropped from disk via atomic tmp + rename.

---

## Lifecycle

```
Proxy start
  │
  ├─ DDWatcher.Start(ctx)         ─ hydrate HandledManifestStore + seed walk
  ├─ QuitWatcher.Start(ctx)
  ├─ Emitter.Start(ctx)           ─ kick off replay-drain goroutine
  ├─ Allocator.Init()             ─ open a first speculative bucket
  └─ Allocator.Run(ctx)            ─ drain intake channel → Handle
       │
       ▼
  … build 1 …
  xcbuilddata CREATE  →  DDWatcher.OnStart(Start{hex, action})
                         Allocator.onStart
                            evict spec (emit IDEBackground if had activity)
                            open fresh bucket, derive InvocationID
                            Proxy.SetCurrentBucketID
                            Emitter.EmitSlim
  LogStoreManifest WRITE → DDWatcher.OnEnd(End{uuid, scheme, duration})
                           Allocator.onEnd
                              close bucket as EndMatched
                              Emitter.EmitEnriched (with CAS stats)
                              openSpec (holds trailing CAS traffic)
                              schemeEmittedSinceStart = true
  … (teardown CAS traffic lands on the spec) …

  Quit (xcodebuild process exited)
     Allocator.onQuit
        ancestry == IDE → scheduleIDEFlush (debounce, then emit IDEBackground if spec had activity)
        ancestry == CLI → flushCLI
                           if schemeEmittedSinceStart: drop spec, reopen fresh
                           else: emit CLIImplicit if spec had activity

Proxy stop
  │
  ├─ DDWatcher.Close
  ├─ QuitWatcher.Close
  ├─ Allocator.CloseIntake     ─ Run flushes buffered events and returns
  └─ Emitter.Stop               ─ drains the pending-ndjson one final time
```

---

## Identity derivation

Per-build InvocationID is a SHA-256 hash over a `buildidentity.Anchor`, reshaped into UUIDv4 form (`internal/xcelerate/buildidentity/buildidentity.go`):

```go
Anchor{ Hostname, PID, StartTimeMS, BuildRequestHex }
```

The PID + StartTimeMS source depends on the bucket type:

| Bucket | PID / StartTimeMS source | BuildRequestHex |
|---|---|---|
| `newBucket` (Start-paired real build) | proxy (`ProxyPID` / `ProxyStartMS`) | `Start.BuildRequestHex` |
| `mintEndOnlyBucket` (sole-End warm rebuild) | proxy | `End.UniqueIdentifier` |
| `ideBackgroundBucket` | peer (from Quit) | `AnchorSaltIDEBackground` |
| `cliImplicitBucket` | peer (from Quit) | `AnchorSaltCLIRun` |
| `openSpec` (per-cycle speculative) | proxy | `fmt.Sprintf(anchorSaltSpecFmt, at.UnixNano())` |

Peer anchoring (IDE / CLI) uses the `PID` / `StartTimeMS` from the QuitWatcher's `Quit{}` payload, not the proxy's. Two IDE sessions with the same PID but different start-ms therefore derive distinct InvocationIDs — PID recycling is a real risk on long-lived desktops. `ideBackgroundBucket` and `cliImplicitBucket` fall back to the proxy's `PID` / `StartTimeMS` when the Quit payload has none; that path should only fire when the QuitWatcher never learned a peer identity, so an unexpected collapse under one ID is worth a Debug-level trace.

---

## Observing the flow

Structured proxy log:

```
~/.local/state/xcelerate/logs/proxy-<uuid>-out.log
```

Grep for the usual milestones. The first block works at the default log level; the second block needs `DebugLogging=true` in `~/.bitrise-xcelerate/config.json`:

```bash
# info-level events
grep 'Allocator: bucket opened\|Allocator: bucket closed\|Allocator: spec-promote End\|Allocator: sole-End mint' proxy-*.log
```

```bash
# debug-level events (require DebugLogging=true)
grep 'Emitter: slim PUT\|Emitter: enriched PUT' proxy-*.log
grep 'flushCLI suppressed' proxy-*.log
grep 'reason=non-primary' proxy-*.log            # Signing / Packages / Localization sub-builds
```

Flush the pending replay queue on demand:

```bash
bitrise-build-cache xcelerate flush-session
```

---

## Testing locally

See `~/.claude/skills/rde-xcode-cas-test/SKILL.md` for the RDE smoke harness. Minimum per-build semantic gates:

- Exactly one scheme-matched bucket per logical build.
- Exactly one enriched PUT per that bucket.
- `shortCommand` carries the scheme bracket `build [<Scheme>]`.
- `cacheHitRate` in the payload matches the xcactivitylog `note: N hits / M cacheable tasks (P%)` diagnostic.
- No `"ide background"` lowercase substring (should only be `"Xcode IDE background"`).
- CLI builds: no secondary `BucketReasonCLIImplicit` row alongside the scheme bucket (suppression verified).

---

## Known gaps

| Topic | Status |
|---|---|
| `testPlan` in `shortCommand` on wrapperless | not parsed from manifest signature; `xcactivitylog`-side extraction is a follow-up |
| Parallel ⌘B on different schemes | Allocator is single-slot; Xcode serializes per workspace so single-user OK; cross-workspace concurrent builds tracked as follow-up |
| Recursive watch on CGO-0 release | not re-validated in this PR; the in-tree `internal/xcelerate/wrapperless/recwatch` adapter layers synthetic walk-on-CREATE replay on top of `fsnotify`, replacing the former CGO-requiring `rjeczalik/notify` |
| `LogStoreManifest.plist` as the only end-signal source | authoritative per Xcode 11-27; reconfirmed on Xcode 27.2 but not formally contracted |

See the repo's open issues for the live list.
