# Xcode toolchain — roadmap

Follow-on work for the toolchain approach documented in
[`xcode-toolchain-approach.md`](xcode-toolchain-approach.md). None of the items
below block the current PR.

## v2 — Local IDE SPM reach once Xcode 27.1 ships

The toolchain already carries `COMPILATION_CACHE_REMOTE_SERVICE_PATH` in its
`OverrideBuildSettings`. The CLI side is complete today.

What's missing is SwiftBuild's `.cas-config` emission picking up that setting
in IDE builds. Apple's swift-build commit `14cb1b0989b8` (merged 2026-07-24)
adds the field to the `.cas-config` `PluginOptions` map; the fix is expected
to ship in Xcode 27.1.

### When Xcode 27.1 lands

1. Verify on a local build with a single Xcode-27.1 install that the proxy
   receives connections tagged with SPM package targets (compare per-target
   hit rates in the local analytics dashboard).
2. Update the coverage matrix in `xcode-toolchain-approach.md` — flip the
   "IDE SPM" cell from pending to yes.
3. Update the `xcode-app-ide-remote-cas-findings-2026-09-30.md` historical
   note to point at the resolved state.

No CLI code change is expected.

## v3 — Proxy-side peer-PID session correlation

Today's enrichment path (introduced in PR #573) parses
`compilation-cache-hit-rate` out of each `.xcactivitylog` after a build
completes and attaches it to the wrapperless invocation PUT. It works but has two
weaknesses:

- **Latency.** The log is only readable once Xcode has flushed and sealed it.
  The handler waits with exponential backoff up to a hard 2-second cap; most
  real builds settle inside 100ms, but the ceiling is the compromise.
- **Scope.** The log carries a single build-wide hit-rate line. Per-target or
  per-connection breakdowns need per-RPC observation on the proxy.

The spike (see
[`xcode-toolchain-spike-findings-2026-10-05.md`](xcode-toolchain-spike-findings-2026-10-05.md))
validated the mechanism:

- Wrap the proxy's `net.Listener`. On each `Accept()`, read `LOCAL_PEERPID`
  via `getsockopt(SOL_LOCAL, LOCAL_PEERPID)`.
- Walk the process ancestry (`ps -o ppid=`, `ps -o comm=`) to the first
  `SWBBuildService` ancestor; its parent is `xcodebuild` for CLI builds and
  `Xcode` for IDE builds — a reliable source classifier.
- Mint a session ID keyed on the ancestor PID.
- Aggregate per-session CAS counts from the proxy's existing
  `ProtocolCollector` snapshots.
- Emit one invocation PUT per session when the connection closes or goes
  idle.

Spike confirmed (one CLI build on `/tmp/cas-probe`):

```
peer_pid=71936 ancestry=71936(SWBBuildService)->71934(xcodebuild)->71846(zsh)->…
```

Deliverables for v3:

1. Promote `peerPIDListener` + `ancestryString` from the spike into
   `internal/xcelerate/proxy/`, non-fatal if `getsockopt` fails (fall back to
   current behavior).
2. New `session` package or extension of `internal/xcelerate/enrichment/` to
   hold the per-PID session map + inactivity timer.
3. On session close, aggregate `ProtocolCollector` deltas and emit the
   invocation PUT with proxy-observed hit/miss counts.
4. When v3 is live, retire the xcactivitylog parse enrichment path from
   `internal/xcelerate/enrichment/` — the proxy-side numbers are
   ground-truth and don't need the sealed-log wait.

Open questions worth answering in a v3 planning doc:

- SWBBuildService lifetime across multiple `Cmd-B` presses in one Xcode.app
  session — one session or many? Needs IDE-reach to be live (either Xcode
  27.1 or an interim fix) before this is measurable.
- Idle timeout for a session that goes quiet but doesn't close its connection.
- Whether to retire `.xcactivitylog` parse entirely or keep as cross-check.

## v4 — Signed toolchain distribution

Not pursued today (local install needs no signing). Only revisit if
user demand emerges for installing the toolchain without the CLI — e.g. for
GUI-focused developers who use it with manual `xcode link` runs. Needs:

- Developer ID Application identity.
- Notarization pipeline.
- `.pkg` build + distribution channel decision.
