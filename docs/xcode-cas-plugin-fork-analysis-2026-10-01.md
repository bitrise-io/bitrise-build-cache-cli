# Custom LLVM CAS plugin — what we'd gain over Apple's stock plugin

Follow-up to `docs/xcode-app-ide-remote-cas-findings-2026-09-30.md`. That doc established that Apple's stock `libToolchainCASPlugin.dylib` already talks to the Bitrise xcelerate proxy over the LLVM-CAS gRPC protocol — so we do NOT need to ship a plugin for the basic "IDE builds engage remote CAS" story. This doc catalogs what we would gain if we DID ship one, modeled on Tuist's Rust fork (`tuist/tuist/cas-plugin/`).

Primary source: [`tuist/tuist/cas-plugin/AGENTS.md`](https://github.com/tuist/tuist/blob/main/cas-plugin/AGENTS.md). Everything in quotes below is lifted verbatim from that file.

## Why Tuist did it (one sentence)

> "Xcode's own remote-caching mode (`COMPILATION_CACHE_REMOTE_SERVICE_PATH`, the gRPC socket its built-in remote mode uses) is net-negative on deep module graphs: the remote choreography inside Apple's closed plugin stalls in 30-50s bursts, taking a 274s no-cache build to 325-445s even with all cache hits served in ~2ms."

Bitrise's xcelerate proxy implements exactly `COMPILATION_CACHE_REMOTE_SERVICE_PATH`'s protocol. If their measurement holds, Bitrise customers with deep module graphs will see the same regression. That is the single strongest "ship our own plugin" argument.

## Wins ranked by impact for Bitrise

### 1 — Side-step the Apple plugin's remote stalls (BIG)

The build system runs in local-only mode; all remote traffic lives inside the custom plugin. Tuist's numbers for the same fixture after switching:

- Warm remote: **113.6s vs 105.5s local-replay floor** (~8s overhead).
- Shallow-graph app project: **50.4s cached vs 87.8s no-cache**, at 50.1s floor.

Our risk is identical because we present the same wire protocol Apple's plugin stalls against. We will not know how bad the regression is on customer projects until a) we measure one or b) a customer reports it. We should at least set up a benchmark on a deep-graph fixture (one of the Samsara / Bitwarden fixtures we already run) with and without our proxy's remote mode engaged.

### 2 — Non-blocking resolve (MEDIUM-BIG)

Xcode's task-setup phase probes thousands of keys. Apple's path blocks per key. Tuist's protocol v2 splits "task-setup probe" (`globally = false`, local-only, never waits for network) from "cache-query task" (global, can prepare the graph). Returns candidates immediately; validates and materializes in background.

For us: compilations stall waiting on round-trips today. On 60ms RTT (US<>EU) this stacks. Our proxy's own prefetcher doesn't help here because the plugin blocks on the gRPC call.

### 3 — Instance-wide snapshot pre-fetch (BIG for cold machines)

> "At startup (for every instance the persisted registry knows) or on an instance's first resolve, the proxy fetches, in the background, kura's instance-wide snapshot — the complete key→value map with a deduplicated node table, served through a reserved action key (`tuist-actioncache-snapshot/v2`) on the ordinary `GetActionResult` surface."

Served via a special sentinel action key — so the server surface doesn't change, old servers answer not-found. Once ready, every resolve answers locally from it. One round trip instead of per-key.

Measured impact: "a completely cold machine — no keylog, no prior build, an agentic sandbox — resolve like a warm one". And the counter-example without it: "702s warm vs an 88s floor at ~60ms RTT".

For us: cold-CI runs and new developer machines pay per-key RTT today. Snapshot would be strictly backend+protocol work on the Accelerate side plus a plugin to consume it. Infrastructure cost on the server end is real (building the snapshot, serving it fresh), but the client side is just REAPI with a sentinel key.

### 4 — Server-inlined blob responses (MEDIUM, bandwidth win)

Tuist's server inlines blob bytes in the `GetActionResult` response (`inline_output_files: ["*"]`). Single RPC returns manifest + bytes. `BatchReadBlobs` only when server didn't inline (budget exhausted / older server).

For us: this is a server extension (needs Accelerate to support the inline hint) plus client code. Pure byte-count win on cache-hit path.

### 5 — REAPI parity (NEUTRAL, already have it)

Tuist talks the Bazel Remote Execution API over gRPC/HTTP-2. Our Accelerate backend also speaks REAPI (we already generate from the Bazel protos). This is parity, not a win over our stack; it's a win over Apple's plugin which speaks Apple's own CAS gRPC. We'd keep REAPI if we built our own plugin.

### 6 — Content-defined chunking (MEDIUM, bandwidth win)

> "Materializers and manifest repairs share in-flight blob reads by Remote identity and blob hash. They must not independently download the same closure"

Shared in-flight reads + chunked bodies. Edited blobs reuse unchanged chunks. Our proxy currently has `cas` + `kv` services with whole-blob transfer; no cross-version chunk reuse. Minor on cold builds, meaningful on warm/edited.

### 7 — CI upload-wait semantics (MEDIUM-BIG for CI UX)

> "Off a runner a CI job's store, and usually its machine, go away with the job, so a record still spooled when the job ends is an upload that never happens. Unless its store is inside `TUIST_CAS_DRAINED_STORE`, the plugin sends `OP_PUBLISH_WAIT` and the put returns once the proxy answers, for at most 30s."

CI jobs end before background uploads flush. Tuist's `OP_PUBLISH_WAIT` gives per-put sync, bounded to 30s, with a per-remote fail-fast breaker so a stalled remote doesn't take 30s×N. Not applied on Tuist runners (they drain explicitly at teardown).

For us: today Bitrise CI jobs can finish with the xcelerate proxy still holding spool records. Those uploads are lost. We have no equivalent of `OP_PUBLISH_WAIT`. A customer measuring "my second build didn't hit" might be hitting this.

### 8 — Store size management (MEDIUM, operational hardening)

> "`COMPILATION_CACHE_LIMIT_SIZE` does not cap the store directory: measured on Xcode 26.5, setting the limit and writing 3x past it prunes nothing"

Apple's limit setting is a lie. Tuist does their own `prune_ondisk_data` call via `OP_PRUNE` from the proxy. Chain of `v1.1`, `v1.2`, … directories; rotates on handle-close.

For us: customer developer machines will fill their disks. Not our problem today (the local store is Xcode's responsibility) — but if we're invested in the IDE path, this bites our story.

### 9 — Per-blob analytics attribution (SMALL-MEDIUM for product telemetry)

> "`src/analytics.rs` — the proxy's per-node transfer analytics, written to `cas_analytics.db` (bundled SQLite, WAL, background writer) in the schema the Swift `CASAnalyticsDatabase` defines. The server joins build-log node id → `nodes.checksum` → `cas_outputs.key`."

They write a SQLite db the Tuist server joins against build-log remarks (`using CAS output`). Attribution per compiler task — which cache keys mapped to which build artifacts.

For us: today we log at proxy level only. If we want per-target cache-hit breakdowns in the Bitrise dashboard, we need this hook — and the hook requires being inside the plugin so we see the compiler's node ids.

### 10 — Endpoint re-resolution (SMALL, correctness)

Daemon re-resolves `TUIST_CAS_REMOTE_GRPC_URL` on account region moves. Launchctl-started proxy would otherwise die when its region drains.

For us: Bitrise Accelerate endpoints are per-workspace and could move. Not urgent, becomes a bug when it bites.

### 11 — macOS endpoint-security / privacy-permission resilience (SMALL, operational)

> "Any `open(2)` [in a CAS directory] can block indefinitely, so the proxy's own spool file work runs where nothing a client waits on depends on it. [...] macOS privacy permissions or endpoint-security software holding the proxy's file access in that directory is the leading suspect."

Full Disk Access denied → whole proxy hangs. Tuist detects stalls, releases handle on background thread, logs operator advice.

For us: enterprise macOS fleet (MDM-managed, Jamf, FDA-locked) will hit this. We'd hang today.

### 12 — Verified downloads + poisoned-put survival (SMALL-MEDIUM, correctness)

Every restored blob sha256-verified before entering the local CAS. Refused puts (`cache poisoned`) report success and still publish remotely, so the build doesn't fail and the recompile fixes it.

For us: today a corrupted-blob roundtrip would silently feed bad bytes to the compiler. Low-probability but it exists.

### 13 — Snapshot staleness detection (ONLY RELEVANT if we add snapshot)

Needed only if we build item 3. Not a standalone win.

## What a Bitrise fork would NOT need from Tuist's design

- **`tuist-instance` plugin option.** Tuist multiplexes many projects on one proxy. Our proxy today runs per invocation anyway; and if we go per-machine, the Bitrise workspace id goes in config, not plugin options.
- **The `TUIST_CAS_REMOTE_GRPC_URL` + `TUIST_CAS_TOKEN` env plumbing.** We have `BITRISE_BUILD_CACHE_AUTH_TOKEN` + `BITRISE_BUILD_CACHE_WORKSPACE_ID` already resolved by the CLI.
- **`tuist cache config` cross-process endpoint resolver.** Our account→endpoint mapping is simpler; the auth layering doc describes it.
- **Runner-image specifics** (`TUIST_CAS_DRAINED_STORE`, snapshot staging on cache volume). Our runners work differently.
- **Analytics schema compat with Tuist server.** Design our own schema to fit the Bitrise insights pipeline.

## Verdict

Shipping our own plugin would be a **big, multi-month commitment** (Rust cdylib, FFI around Apple's plugin, REAPI client, protocol design, proxy rewrite in that language or an IPC bridge, store management, tests). The current `xcode-app enable + link` surface we just shipped on PR #568 is the correct cheap path AND does not foreclose any of the above.

A plugin becomes a priority IF ANY of the following happens:

1. A customer reports the Apple-plugin stall (item 1). Measurable on a deep-graph fixture; worth a timeboxed benchmark **this quarter**.
2. We decide snapshot-based cold-start (item 3) is a strategic feature.
3. CI uploads lost at job teardown (item 7) becomes a visible drop in warm-build hit rate.
4. We want per-blob attribution in the dashboard (item 9).

Until then, the stock-plugin path is the right default. Revisit after we have measured item 1 on at least one production-shaped fixture.

## Measurement next step (cheap, do this soon)

Pick an existing CI fixture with heavy module graphs — Samsara `installDriverDebug`, bitwarden assert-build-1 — and run three variants:

1. No cache (baseline).
2. Our wrapper CLI path, remote CAS engaged via `COMPILATION_CACHE_REMOTE_SERVICE_PATH` (today's shipping behavior).
3. Same, with the proxy socket pointed at `/dev/null` so remote never engages (local plugin only).

Compare wall-clock. If (2) is slower than (1) and (3) is faster than (1), we have the Tuist regression on our hands.

## References

- `docs/xcode-app-ide-remote-cas-findings-2026-09-30.md` — the prior investigation establishing Apple's plugin works with our proxy.
- `docs/xcode-app.md` — the user-facing `xcode-app enable / disable / link / unlink` doc.
- [`tuist/tuist/cas-plugin/AGENTS.md`](https://github.com/tuist/tuist/blob/main/cas-plugin/AGENTS.md) — primary source for every claim in this doc.
- [`tuist/tuist/server/priv/docs/en/guides/features/cache/xcode-cache.md`](https://github.com/tuist/tuist/blob/main/server/priv/docs/en/guides/features/cache/xcode-cache.md) — the user-facing Tuist guide.
- Tuist PR #12968 (cold-replay benchmark) and the measured regression numbers referenced in `AGENTS.md`.
