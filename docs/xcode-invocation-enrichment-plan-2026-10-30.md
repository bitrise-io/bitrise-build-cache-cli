# Xcode Invocation Enrichment — Go-native Port Plan (2026-10-30, rev3)

Companion to `docs/xcode-invocation-enrichment-survey-2026-10-30.md`. High-level architecture for an in-house Go collector replacing/augmenting what XCMetrics + XCLogParser give the Swift world. All collectors opt-in. Test-side data explicitly out of scope.

**Rev3 (post second architect pass):** package renamed `enrich` → `xcprobe` to avoid cognitive collision with `enrichment/`; host snapshot composes `common.HostMetadata` instead of extending it (keeps the common type Bazel-sized); `Collector` interface decoupled from `xceleratconfig` (aggregator owns the enabled-set decision); payload type owned by `payload/`, surfaced on `analytics.Invocation` as `json.RawMessage` to avoid a cycle; PR 594 types for build-identity / derived-data correlation named provisionally with a merge-defer note.

## Shape

```
                    ┌─────────────────────────────┐
                    │ cmd/xcode (wrapper + wrpless)│
                    └─────────┬───────────────────┘
                              │ opt-in config (CLI > env > file)
                              ▼
          ┌───────────────────────────────────────────────┐
          │ internal/xcelerate/xcprobe (new sibling of    │
          │ internal/xcelerate/enrichment/)               │
          │                                               │
          │  ┌──────────────┐  ┌──────────────────────┐  │
          │  │ flaginject   │  │ host (Snapshot       │  │
          │  │ (Phase 3+)   │  │  COMPOSES            │  │
          │  │              │  │  common.HostMetadata │  │
          │  │              │  │  + Xcode-only probes)│  │
          │  └──────────────┘  └──────────────────────┘  │
          │                                               │
          │  ┌───────────────────────────────────────┐   │
          │  │ slf (Phase 1+, Go SLF parser)         │   │
          │  └───────────────────────────────────────┘   │
          │                                               │
          │  ┌───────────────────────────────────────┐   │
          │  │ collect (one Collector per source)    │   │
          │  │   Phase 0: hostsnap, buildtiming,     │   │
          │  │            buildsettings              │   │
          │  │   — no config import; aggregator      │   │
          │  │     passes a per-collector enabled    │   │
          │  │     bool                              │   │
          │  └───────────────────────────────────────┘   │
          │                                               │
          │  ┌───────────────────────────────────────┐   │
          │  │ payload                                │   │
          │  │   EnrichmentPayload (owns the type)   │   │
          │  │   Marshaled to json.RawMessage and    │   │
          │  │   stored on analytics.Invocation.     │   │
          │  │   Enrichment (field typed json.Raw-   │   │
          │  │   Message — one-way edge)             │   │
          │  └───────────────────────────────────────┘   │
          └───────────────────┬───────────────────────────┘
                              │
         ┌────────────────────┼────────────────────┐
         │                                         │
         ▼                                         ▼
┌─────────────────────┐           ┌─────────────────────────────┐
│ cmd/xcode/           │           │ internal/xcelerate/         │
│   xcodebuild.go      │           │   enrichment/enricher.go    │
│   (wrapper path)     │           │   (wrapperless / IDE path)  │
└─────────────────────┘           └─────────────────────────────┘
         │                                         │
         └────────────────────┬────────────────────┘
                              ▼
                 analytics.Invocation + .Enrichment (json.RawMessage)
                       → PutInvocation (unchanged BE contract)
```

## Package layout

```
internal/xcelerate/xcprobe/
  slf/                  (Phase 1+, no code in Phase 0)
    reader.go
    classes_v{8,10,11}.go
    buildstep.go
    attachment.go
  host/
    snapshot.go         (Phase 0) type Snapshot struct { common.HostMetadata; Thermal...; MemFreeMB...; ... }
                        Capture(ctx, base common.HostMetadata) Snapshot — composes, never mutates base
  collect/
    collector.go        Collector interface (no config imports); Input{DerivedDataPath, WindowStart/End, XcodebuildInfo, HostBase, BenchmarkPhase}; Output{Host, BuildTimings, BuildSettings}
    hostsnap.go         (Phase 0) wraps host.Capture
    buildtiming.go      (Phase 0) regex over -showBuildTimingSummary tail
    buildsettings.go    (Phase 0) thin wrapper over xcodebuildinfo.Client's new ShowBuildSettings method
    (later phases add xcactivitylog.go, swiftstats.go, clangtrace.go, linkerstats.go, builddb.go)
  payload/
    enrichment.go       EnrichmentPayload + sub-rows (SchemaVersion, CollectorErrors, Host, BuildSettings, BuildTimings)
    emit.go             AttachToInvocation(inv *analytics.Invocation, p EnrichmentPayload) — marshals to json.RawMessage, assigns inv.Enrichment, enforces 64 KB soft cap
  aggregator.go         Reads EnrichmentOptions, builds enabled-set of collectors, runs each in a goroutine under PerCollectorTimeout, merges Outputs into a payload. Only this file imports internal/config/xcelerate.

internal/config/xcelerate/
  enrichment_options.go EnrichmentOptions struct + file loader + env loader + CLI flag binding helpers
  enrichment_options_test.go

internal/config/common/
  cache_config.go       UNCHANGED. HostMetadata stays Bazel-sized.
  benchmark.go          UNCHANGED. BenchmarkPhaseProvider reused by aggregator for the gate.

internal/xcelerate/xcodebuildinfo/
  xcodebuildinfo.go     EXTENDED. Client gains ShowBuildSettings(ctx, scheme, project) ([]byte, error) running `xcodebuild -showBuildSettings -json`

internal/xcelerate/analytics/
  types.go              EXTENDED minimally. Invocation gains Enrichment json.RawMessage `json:"enrichment,omitempty"`. No typed import — avoids cycle with payload/.
```

Dependency direction (checked by `make lint-arch`):
- `host/` → `internal/config/common` (reads `HostMetadata` as compose base)
- `collect/*` → `host`, `internal/xcelerate/xcodebuildinfo`, `payload` (for Output struct types only)
- `payload` → `encoding/json`, `internal/xcelerate/analytics` (writes to `Invocation.Enrichment json.RawMessage` — one-way, no reverse import)
- `aggregator` → `collect`, `payload`, `internal/config/xcelerate`, `internal/config/common` (BenchmarkPhaseProvider). Only aggregator imports the xcelerate config package.
- `cmd/xcode` → `aggregator` + `internal/config/xcelerate`
- `internal/xcelerate/enrichment/enricher.go` → `aggregator` (new import — one line in enricher's existing emit path, placed AFTER `attachXcresultSummary` so enrichment sees a fully-populated invocation)

No cycle: `analytics` does not import `payload`, `payload` imports `analytics` only.

## Opt-in config (lives under `internal/config/xcelerate/`)

```go
package xceleratconfig

type EnrichmentOptions struct {
    HostSnapshot        bool
    BuildTimingSummary  bool
    BuildSettings       bool

    ActivityLog         bool
    ActivityLogAttach   bool

    SwiftFunctionTimes  bool
    SwiftTypeChecks     bool
    SwiftStats          bool
    ClangTimeTrace      bool
    LinkerStats         bool

    BuildDB             bool

    SwiftTopN           int
    ClangTopN           int

    PerCollectorTimeout time.Duration
    FailOpen            bool
}
```

Precedence (low → high): config file → env (`BITRISE_BUILD_CACHE_ENRICH=hostsnap,buildtiming,...`, plus `BITRISE_BUILD_CACHE_ENRICH_ALL=true` / `=none`) → CLI flags on `xcode activate` + `xcode xcodebuild` (`--enrich=…`, `--enrich-all`, `--enrich-none`).

Benchmark-phase gate: `common.BenchmarkPhaseProvider` is passed into the aggregator. When phase == `baseline`, Phase-3+ flag-injection collectors are forced off even if explicitly enabled (log a warn). Phase 0 collectors are not gated — they do not perturb the build.

## SLF parser plan (Phase 1+, out of scope for Phase 0)

Pure Go. `compress/gzip` + custom tokeniser. Mirrors XCLogParser's eight-token grammar. Class property order shipped per `(class, slfVersion)` tuple. Golden testdata per Xcode major.

**Retirement contract:** `internal/xcactivitylog/reader.go` (current `CompilationCacheMetrics` regex-reader) is Phase 1's replacement target. Phase 1 PR greps every call site (`cmd/xcode/check_xcode_recent_build.go`, `internal/xcelerate/enrichment/enricher.go`), routes through the new SLF decoder, deletes the old reader in the same PR. Not a Phase 0 concern.

## Host snapshot — composition, not extension

```go
package host

type Snapshot struct {
    common.HostMetadata                  // embedded — base stays untouched
    Thermal              *ThermalInfo   // pmset -g therm
    MemFreeMB            uint64         // vm_stat free + inactive
    SwapTotalMB          uint64
    SwapFreeMB           uint64
    UptimeSeconds        uint64         // sysctl kern.boottime
    IsVirtual            bool           // sysctl kern.hv_vmm_present
    SpotlightActive      bool           // mdutil -s /
    TimeMachineActive    bool           // tmutil status
    ModelID              string         // system_profiler cached across session
    Chip                 string
}

func Capture(ctx context.Context, base common.HostMetadata) Snapshot
```

`common.HostMetadata` is not touched. Hot paths (Bazel/Gradle/ccache activation) continue to call `common.NewMetadata` and get the Bazel-sized struct. Only Xcode enrichment opts into the extra probes.

## Collector contract — zero coupling to config

```go
package collect

type Collector interface {
    Name() string
    Collect(ctx context.Context, in Input) (Output, error)
}

type Input struct {
    DerivedDataPath string
    Project         string
    Scheme          string
    WindowStart     time.Time
    WindowEnd       time.Time
    XcodebuildInfo  *xcodebuildinfo.Client
    HostBase        common.HostMetadata     // the already-populated baseline; hostsnap composes
    BenchmarkPhase  common.BenchmarkPhase
    // Phase 1+ additions (nil in Phase 0):
    // ManifestEntry  *enrichment.LogStoreManifestEntry
    // BuildIdentity  *buildidentity.Identity      — provided by PR 594
    // ConnectionSession *proxy.ConnectionSession  — provided by PR 594
}

type Output struct {
    Host           *payload.HostSnapshot
    BuildTimings   *payload.BuildTimingHistogram
    BuildSettings  *payload.BuildSettingsSnapshot
}
```

- `collect/` imports neither `internal/config/xcelerate` nor `internal/config/common.BenchmarkPhase` beyond the type on `Input`. Aggregator decides which collectors are enabled from `EnrichmentOptions` and only instantiates those; collectors have no self-awareness of config.
- Interface lives in `internal/xcelerate/xcprobe/collect/` — `internal`-only. No `pkg/` leak.
- Each collector runs in its own goroutine under `in` context + `PerCollectorTimeout`. One failure never kills another (`FailOpen=true` by default). Errors go to `payload.CollectorErrors[name] = err.Error()`.
- `Collect` is pure read. No mutation of DerivedData.

## PR 594 integration contract

PR 594 introduces canonical sources for the two most important `Input` fields:
- `internal/xcelerate/buildidentity/` → `buildidentity.FromAncestry(...)` returns the authoritative BuildIdentity (PID + start-ns anchoring).
- `internal/xcelerate/proxy/derived_data_path.go` → `connection_session.DerivedDataPath()` resolves the correct DerivedData root for a given session.

Phase 0 ships with provisional extraction (DerivedData from env / activation config, window from `xcodebuild` wall-clock). The commits mark these call sites with `// xcprobe-TODO: switch to buildidentity.FromAncestry once PR 594 merges` so the swap is grep-able.

**Merge order:** this branch stays local until PR 594 is merged. On open, Phase 0's PR targets `main` (post-594) and the provisional extraction is swapped for the canonical plumbing in the same PR. If PR 594 is still open at open-time, this PR targets `fix/aci-5623-manifest-sidecar-convergence` and the swap happens on rebase.

## Payload shape — payload/ owns the type

```go
package payload

type EnrichmentPayload struct {
    SchemaVersion   int                `json:"schemaVersion"`
    CollectorErrors map[string]string  `json:"collectorErrors,omitempty"`

    Host           *HostSnapshot           `json:"host,omitempty"`
    BuildSettings  *BuildSettingsSnapshot  `json:"buildSettings,omitempty"`
    BuildTimings   *BuildTimingHistogram   `json:"buildTimings,omitempty"`
}

// AttachToInvocation marshals the payload and writes it to inv.Enrichment
// (a json.RawMessage on analytics.Invocation). Returns an error only if
// marshaling fails; enforces a 64 KB soft cap by dropping the largest
// sub-row before marshaling if needed.
func AttachToInvocation(inv *analytics.Invocation, p EnrichmentPayload) error
```

`analytics.Invocation` gains:

```go
Enrichment json.RawMessage `json:"enrichment,omitempty"`
```

No typed import of `payload.*` from `analytics` — zero cycle risk, zero knowledge transfer. BE already tolerates unknown JSON fields; sibling-stream decision deferred until Phase 1 brings per-target rows.

**Explicit non-goal for Phase 0:** no sibling stream, no new `PutEnrichment` endpoint. Phase 0 payload cap: scalar + histogram sub-rows, bounded by step-type count (<20).

## Where it plugs in

Two hook sites — both call the same aggregator:

**Wrapper path — `cmd/xcode/xcodebuild.go`.** After `xcodebuild` returns, before `saveInvocationAndRelation`, build an `collect.Input` from the invocation window + already-known DerivedData path, run `aggregator.Run(ctx, opts, in)`, call `payload.AttachToInvocation(inv, out)`. Behind an `EnrichmentOptions.AnyEnabled()` guard — zero cost when all off.

**Wrapperless / IDE path — `internal/xcelerate/enrichment/enricher.go`.** Same aggregator call, placed in the existing `PutInvocation` emit site, **after `attachXcresultSummary`** so enrichment sees the fully-populated invocation. Behind the same guard.

Phase 0 PR touches `enricher.go` for a single call-site addition, no logic change — minimizes merge risk against the active PR stack (593/594/596/610).

## Flag injection (Phase 3+ only — no code in Phase 0)

Phase 0 ships no `flaginject` code. The benchmark-phase gate is wired through the aggregator for Phase 0 (reads from `common.BenchmarkPhaseProvider`); Phase 3 adds the first collectors that need to be gated.

## Phasing

- **Phase 0** (this PR) — scaffold `internal/xcelerate/xcprobe/{collect,payload,host}` + `aggregator.go`. Three always-opt-in collectors: `hostsnap`, `buildtiming`, `buildsettings`. `EnrichmentOptions` in `internal/config/xcelerate/`. Hook on both wrapper + IDE paths. Nested `json.RawMessage` payload on `analytics.Invocation`. All collectors default OFF.
- **Phase 1** — SLF reader + xcactivitylog collector. Replaces `internal/xcactivitylog/reader.go` in a single-PR move.
- **Phase 2** — SLF v11+ attachment decoder (CompilationCacheMetrics typed struct).
- **Phase 3** — flag-injection collectors (`swifttypes`, `swiftfuncs`, `clangtrace`, `linkerstats`).
- **Phase 4** — `builddb` + swift `-stats-output-dir`.
- **Per Xcode major** — bump SLF class tables + add testdata fixture.

### Splittable sub-PRs within Phase 0 (optional)

If reviewer-friendly splits are preferred, Phase 0 safely decomposes into:
1. `xcodebuildinfo.Client.ShowBuildSettings` (standalone).
2. Scaffold (`collect` + `payload` + `aggregator`) with zero collectors — hooks a no-op aggregator call behind `AnyEnabled() == false`.
3. The three collectors (`hostsnap`, `buildtiming`, `buildsettings`), each independent.
4. Hook-site symmetry PR (wrapper + enricher.go) — must land as one PR to keep both paths behaviorally identical.

For local-only scaffolding this run, everything lands on one commit series on `feat/xcenrich-phase0-scaffold`; the split is a release-time concern.

## Stacking + rebase plan

Phase 0 branch `feat/xcenrich-phase0-scaffold` is rooted at the tip of `fix/aci-5623-manifest-sidecar-convergence` (top of the Xcode IDE stack — PRs 568/593/594/595/596/610). Local only; no push, no PR.

- When ancestor stack merges to main, rebase.
- If an ancestor PR adds a conflicting signature on `enricher.go`'s emit site, resolve in favor of the ancestor and re-apply the one-line aggregator call.
- `enrich.Input`'s provisional DerivedData/window extraction gets swapped for PR 594's `buildidentity` + `connection_session` canonical plumbing on rebase.
- Do NOT front-run the stack — Phase 0 landing before #593 merges would create a merge-conflict chain blocking the whole stack.

## Risks + mitigations (rev3 additions in **bold**)

- SLF drift each Xcode major → vendor class tables per-version; fail-open on unknown; golden-test per major.
- Flag injection perturbs build → benchmark-phase gate; all off by default; `--enrich-none` kill switch.
- Payload size on large codebases → top-N caps on every list; soft 64 KB cap in `AttachToInvocation`; sibling stream deferred.
- Enricher ancestry rejection on IDE → hook sits at the already-ancestry-gated `enricher.go` emit site.
- Private framework reliance → class tables derived from Apache-2 XCLogParser code — no runtime dep on private Apple frameworks.
- Collector duplication → **`host.Snapshot` composes `common.HostMetadata`; no field extension; hot paths unaffected.** `xcodebuildinfo.Client` extended in-place for `ShowBuildSettings` (small, additive).
- Collision with in-flight PR stack → stacking plan explicit; `enricher.go` touch is one-line; PR 594 types named with `xcprobe-TODO` markers for a grep-based swap.
- BE schema → `enrichment` is `json.RawMessage` on `Invocation`, omit-when-empty, BE tolerates unknown fields.
- **Package naming collision** → renamed `enrich` → `xcprobe` (short, no overlap with `enrichment/`).
- **`collect` ↔ config coupling** → `collect` imports no config; aggregator is the sole config consumer.
- **`analytics` ↔ `payload` cycle risk** → `analytics` field typed `json.RawMessage`; one-way edge `payload → analytics`.

## Open decisions (none blocking Phase 0 anymore)

1. ~~Nested-blob vs sibling-stream vs hybrid~~ — Phase 0 nested only. Sibling stream re-opened when first unbounded-cardinality data arrives.
2. ~~`baseline` phase hard-disables flag injection or just warns~~ — hard-disable with warn log.
3. ~~Opt-in UX surface~~ — config file + env + CLI flags, CLI > env > file.
4. BE schema ownership for later-phase sibling stream — defer until Phase 1 lands.
5. ~~Package naming~~ — `xcprobe`.
6. ~~Host-type extension vs composition~~ — composition.
7. ~~Config coupling in `collect`~~ — zero coupling; aggregator owns.
8. ~~Payload-type ownership~~ — `payload/`; `analytics.Invocation.Enrichment` is `json.RawMessage`.
