# xcprobe IDE-path RDE findings (2026-10-30)

Context for the agent running on RDE session `05d7ae45-018d-4c1b-95de-2b59e78d0fcf` (cas-local-ide-caching-exp-27.0). Written while debugging why the enricher-side xcprobe EnrichmentHook never fired on a fresh RDE smoke test, so you can skip the same wall-walk.

## What I set out to prove

Phase 0+1+2 of `feat/xcenrich-phase1-phase2-slf` wires xcprobe collectors to both:
- Wrapper hook: `cmd/xcode/xcodebuild.go` → `attachEnrichment`
- IDE hook: `internal/xcelerate/enrichment/enricher.go` → `e.EnrichmentHook(ctx, manifestPath, group, host, benchmarkPhase, inv)` at line ~180, factory in `cmd/xcode/enrichment_hook.go#newEnrichmentHook`.

Patched both to also `os.WriteFile("/tmp/enrichment-{wrapper,ide}.json", inv.Enrichment, 0644)` after `AttachToInvocation`.

## Wrapper hook — PROVEN

Fresh RDE Xcode 26.6, `/Users/vagrant/HelloCache` xcodegen project, `BITRISE_BUILD_CACHE_ENRICH=hostsnap,buildtiming,buildsettings,activitylog,activitylogattach`.

`/tmp/enrichment-wrapper.json` populated — 9 DetailStepTypes, 25 top steps, host snapshot, attached to real `PutInvocation` payload (BE 401'd on auth; payload shape proven in trace).

Known gaps surfaced in the wild:
- `buildSettings` collector no-ops because wrapper's `collect.Input.XcodebuildInfo` is nil (reviewer's residual #2 in `docs/xcode-invocation-enrichment-plan-2026-10-30.md`).
- `buildTimings.buckets` populated only when `collect.Input.XcodebuildInfoBytes` is pre-captured `-showBuildTimingSummary` output.
- `host.Snapshot` embeds `common.HostMetadata` → mixed camelCase/PascalCase wire shape (reviewer's residual #1).

## IDE hook — NOT fired on 26.6, root cause ≠ ancestry

I believed on 26.6 the enricher hook never fired. **The root cause is probably NOT the ancestry gate.** Walked the proxy debug log carefully; the sequence I observed on every stock-xcodebuild run was:

```
SetSession called with invocation ID: <uuid>
Invocation saved. Visit 👉 https://app.bitrise.io/build-cache/invocations/xcode/<uuid>
Enrichment PUT skipped for <uuid>: pending record already claims this invocation
```

The `Correlate(span, pending) → match → return` branch at `enricher.go:114-123` short-circuits BEFORE the hook call at `enricher.go:180`. The pending record was written by the wrapper's own `PutInvocation` path (via `cmd/xcode/xcodebuild.go`), because — critical gotcha — on an activated host, `which xcodebuild` resolves to `~/.bitrise-xcelerate/bin/xcodebuild`, which is a bash shim that unconditionally dispatches to `bitrise-build-cache xcelerate xcodebuild "$@"`. So every bare `xcodebuild` call I ran was the wrapper path, not the stock CLI.

I **never** actually ran `/usr/bin/xcodebuild` directly on 26.6 to bypass the shim. The whole "IDE hook silent on 26.6" conclusion is from running the wrapper and expecting the enricher to also fire — but the wrapper's correlation short-circuit is working as designed.

## Why your 27.0 session is different

On this session (Xcode 27), the running CLI build at the time I peeked was:

```
26959 /Applications/Xcode-27.0.0.app/.../SWBBuildService
  └─ 25693 /Applications/Xcode-27.0.0.app/.../xcodebuild -project IceCubesApp.xcodeproj -scheme IceCubesApp ... -toolchain io.bitrise.cas.xctoolchain
       └─ 1   launchd
```

`xcodebuild` (NOT the shim; `/Applications/Xcode-27.0.0.app/.../xcodebuild`) spawns `SWBBuildService`. Peer ancestry seen by the proxy becomes something like `[SWBBuildService, xcodebuild]`. `enrichment/sidecar_index.go#acceptedAncestors` currently contains:

```go
var acceptedAncestors = map[string]struct{}{
    "xcodebuild": {},
    "Xcode":      {},
}
```

`xcodebuild` ∈ accepted → the sidecar is kept. **So the gate is NOT the current gap for xcodebuild-via-SWB on 27.** You should already be seeing the IDE hook fire for your builds there — if you aren't, dump the sidecar's `peer_ancestry` from `~/.local/state/xcelerate/sessions/` and compare against the set above.

## Known ancestry rejections to watch for

If your path hits a non-accepted ancestry, you'll see the proxy warn-log:

```
Sidecar <path> ancestry rejected: [a, b, c]
```

Candidates that may show up in the wild and are NOT in `acceptedAncestors` today:
- `SWBBuildService` alone (if the proxy sampled an orphan plugin without catching the Xcode/xcodebuild parent)
- `simctl` / `simulator` / `CoreSimulatorService` for Simulator-driven runs
- `swift-driver` / `swift-frontend` for standalone swiftc shells
- `xcodebuild-stub` names used by test rigs

If any of those actually surfaces, the hotfix is additive — append to the `acceptedAncestors` map. Non-destructive, no behavior change for accepted paths.

## What I'd NOT change blindly

Do not swap the gate to accept-all. The gate exists because:
- Toolchain shim invocations (dsymutil-shim, clang shim) emit sidecars whose cache stats are *for a specific tool*, not the parent xcodebuild — rolling those into an orphan invocation would double-count.
- `project_enricher_ide_ancestry_rejection` fixed one bug by *tightening* ancestry handling, not loosening. Loosening without evidence re-opens that class of bug.

Dump the rejected `peer_ancestry` first; add only the specific strings you need.

## My wrapper-side patch (safe to drop into your worktree)

In `/Users/vagrant/bitrise-build-cache-cli/cmd/xcode/xcodebuild.go`, after `AttachToInvocation` in `attachEnrichment`:

```go
if raw := inv.Enrichment; raw != nil {
    _ = os.WriteFile("/tmp/enrichment-wrapper.json", raw, 0o644)
}
```

In `cmd/xcode/enrichment_hook.go` inside `enrichmentRunFn`, after `AttachToInvocation`:

```go
if raw := inv.Enrichment; raw != nil {
    _ = os.WriteFile("/tmp/enrichment-ide.json", raw, 0o644)
}
```

Rebuild and `cp /tmp/bbcc ~/.bitrise-xcelerate/bin/bitrise-build-cache` to replace the activated binary. Restart proxy. Trigger a build via your IceCubesApp flow. `ls /tmp/enrichment-*.json` tells you which path(s) fired.

## Scratch branch

`feat/xcenrich-phase1-phase2-slf` pushed to `origin`. Rooted on `fix/aci-5623-manifest-sidecar-convergence` (top of PR stack 568/593/594/595/596/610). Payload shape: `analytics.Invocation.Enrichment json.RawMessage`, nested-blob only, 64 KB soft cap, `BITRISE_BUILD_CACHE_ENRICH=hostsnap,buildtiming,buildsettings,activitylog,activitylogattach` or `BITRISE_BUILD_CACHE_ENRICH_ALL=1` to toggle.
