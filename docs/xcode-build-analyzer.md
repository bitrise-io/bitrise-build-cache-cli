# Xcode build-data analyzer

Design sketch for a Go-based post-mortem analyzer that fuses the three
on-disk artifacts Xcode drops by default — build.db, llbuild manifest,
and xcactivitylog — into a single structured report covering: the
planned task graph, the executed tasks with their durations, why each
task ran (cache invalidation reason), and structured failure reasons.

The goal is a Bitrise-side answer to "what actually happened in this
Xcode build and why" without needing XCMetrics, xcpretty, or any Swift
toolchain dependency on the caller side.

## Goals

- **Planned tasks**: enumerate every task Xcode scheduled for this
  build + its tool (swiftc / clang / ld / …), its full argv, its
  declared inputs and outputs, and the target it belongs to.
- **Executed tasks + time**: for each planned task, did it run this
  build and if so, how long did it take and with what outcome.
- **Dependencies**: full input/output graph joining tasks into a DAG.
- **Execution reason**: when a task re-ran, which dependency key's
  signature changed vs the prior successful build.
- **Failure reason**: structured stderr + file:line for any task that
  exited non-zero.

Non-goals (v1): semantic diff of signatures ("this .swift file's
interface changed"), real-time streaming, anything that requires
patching the Xcode build system.

The single-snapshot limitation is lifted by Phase B (landed). B.1 ships a
pure-Go `VACUUM INTO` snapshot store under
`~/.local/state/xcelerate/build-analyzer/snapshots/<build-hex>/<iter>-<ts>.db`
with a 10-snapshot retention cap per build-hex. B.2 teaches the analyzer to
auto-pick the newest snapshot at-or-before the log's finish-time and diff
signatures against the next-older one — so the "whose signature changed
first since the prior build" question has a first-class answer. B.3
(orchestration — auto-trigger snapshot on build completion via fsnotify or
EndWatcher) is deferred to the wrapperless-flow integration where the
proxy already watches DerivedData for other reasons.

## Inputs — what Xcode drops on disk for every build

No flags required. Every Xcode build writes these as a side-effect.

### 1. `build.db` — llbuild incremental state

Path on Xcode 26: `~/Library/Developer/Xcode/DerivedData/<Proj>-<hash>/Build/Intermediates.noindex/XCBuildData/build.db`

**Deviation from the original sketch (v2 implementation):** the DB does NOT
live inside the `<build-hex>.xcbuilddata/` directory next to `manifest.json`
— it sits one level up in `XCBuildData/` and the same DB covers every
`.xcbuilddata` sibling (one row per rule-key, with multiple manifests
sharing the row space). `LocateBuildDB` resolves the parent-level path
directly. If a future Xcode moves it back inside the hex directory, update
that locator.

SQLite database. Source of "did this task's signature change since the
last build". Key tables as observed on Xcode 26.x:

- `key_names (id INTEGER PRIMARY KEY, key STRING UNIQUE)` — interned string
  pool for rule keys. The reader joins against this. Keys carry a one-byte
  rule-kind prefix: `C` for a Command rule (manifest-command-keyed), `N`
  for a Node rule (file / virtual node). The classifier looks up Task
  rules as `"C" + Task.ManifestKey` with raw-key fallbacks.
- `rule_results (key_id, value BLOB, signature INTEGER, built_at INTEGER,
  computed_at INTEGER, start REAL, end REAL, dependencies BLOB)`.
  - `built_at` turned out to be **an iteration counter, NOT a timestamp**
    — a small integer that equals `info.iteration` on the build that
    computed the row and stays stuck at an older value for rules carried
    over unchanged. v2 uses this for the ReasonRanThisBuild /
    ReasonSkippedUpToDate split via iteration equality.
  - `start` / `end` are REAL seconds on a clock that is NOT CoreFoundation
    absolute time (observed range ≈ 7.3 million on an Xcode build whose
    activity-log timestamps decoded to a 2026 wall-clock). The reader
    surfaces `start` as `RuleResult.BuiltAtStart` for callers that want
    per-rule ordering within one build, but classification does not depend
    on it — comparing it to the xcactivitylog wall-clock is incorrect.
  - `signature` is an 8-byte INTEGER (int64 / uint64 bit pattern).
- `info (id, version, client_version, iteration)` — one row carrying the
  current build's iteration counter. `info.iteration` drives the
  classifier.
- `rule_dependencies` — **does not exist as a separate table on Xcode 26**.
  The original design sketch (and the public llbuild docs) suggest an
  `(rule_id, dependency_rule_id)` table, but the actual on-disk encoding
  packs the adjacency into the `rule_results.dependencies` BLOB column: a
  concatenated array of little-endian int64 `key_id` values. Length of the
  BLOB divided by 8 gives the dep count; each int64 is a foreign key into
  `key_names`. The post-v3 attribution reader decodes that blob — see the
  v4 notes below.

v2 explicitly does NOT attempt to distinguish
`signature-mismatch-with-prior` from `new-task` — that needs the prior
build's `build.db` snapshot and we only have the current one. The four
buckets v2 ships are the ones that fall out cleanly from a single-snapshot
read.

### 2. `manifest.json` — static planned task graph

Path: `<build-hex>.xcbuilddata/manifest.json`

**Deviation from the original sketch (v1 implementation):** the file is
**JSON**, not YAML, and the filename is `manifest.json`, not
`manifest-*.xcbuild`. Pre-v1 the design doc trusted XCLogParser prose that
hasn't been updated for current Xcode versions. The structural shape
under `commands` still matches llbuild's model.

Top-level shape:

```jsonc
{
  "client": { … },
  "targets": { … },
  "nodes": { … },
  "commands": {
    "<command-key>": { "tool": …, "description": …, "inputs": […], "outputs": […], "args": […], "signature": … }
  }
}
```

- `commands` is a map keyed by llbuild's internal command name (usually a
  priority prefix `P0:::` / `P2:target-<guid>:Debug:` followed by the same
  text that lives in `description`).
- `tool` is llbuild's internal label: `swift-driver-compilation`, `linker`,
  `shell`, `file-copy`, `phony`, `auxiliary-file`, `create-build-directory`,
  `stale-file-removal`, `embed-swift-stdlib`, etc. v1's JoinManifest uses
  this to **override v0's first-word tool heuristic**.
- `description` is the human-readable label that also surfaces as the
  activity-log section signature (modulo the `(in target 'X' from project
  'Y')` suffix that the activity log appends). This is the **primary join
  key** — the design doc's "signature preferred" is backward in practice:
  only a handful of `shell` commands (ClangStatCache, …) carry a `signature`
  field; the rest rely on description matching.
- Orchestration commands (`phony`, `stale-file-removal`,
  `create-build-directory`, target-dependency aggregation nodes) carry a
  bare `{tool, inputs?, outputs?}` subset with no description. v1 skips
  them so the DAG surfaces real tasks only.

The manifest is the authoritative "what Xcode planned to do" input.

### 3. `.xcactivitylog` — executed tasks + outcomes

Path: `~/Library/Developer/Xcode/DerivedData/<Proj>-<hash>/Logs/Build/<UUID>.xcactivitylog`

Gzipped SLF v11. Source of "did this plan actually run, and with what
outcome". Each executed command is an `IDEActivityLogCommandInvocationSection`
carrying:

- `commandDetail` — matches the manifest's command description.
- `timeStartedRecording` / `timeStoppedRecording` — wall-clock.
- stdout + stderr blobs.
- `exitCode`.
- `emittedOutputText` — structured diagnostics (file:line, severity).

The CLI already has a thin reader at `internal/xcactivitylog/reader.go`
for CompilationCacheMetrics. The analyzer extends this with a full
section walker (not a line scanner — SLF payloads trip bufio.Scanner).

### 4. `*.dia` — clang serialized diagnostics (v3, deferred)

Path: `Build/Intermediates.noindex/.../Objects-normal/<arch>/*.dia`

Clang's binary diagnostic format — an LLVM bitstream stream with a block
hierarchy (META / DIAG) and ABBREV records, starting with a four-byte `DIAG`
magic. A pure-Go reader is viable in principle (the bitstream codec is well
documented), but weighing in at 1–2 days of focused work for a minimal viable
decoder, it was scoped OUT of v3 — the fallback policy explicitly allows it.

Swift's `swiftc` doesn't emit `.dia` regardless, so the stderr extractor is
the sole Swift source either way. v3 ships that extractor; a future phase
revisits the `.dia` reader if the stderr coverage proves insufficient.

## Output

Primary output is a structured JSON report, one object per analyzed
build:

```jsonc
{
  "build": {
    "derivedDataPath": "/Users/me/Library/.../DerivedData/Foo-abc123",
    "xcbuilddataHex": "d3b07384d113edec49eaa6238ad5ff00",
    "activityLogUUID": "A1B2C3D4-...",
    "startedAt": "2026-11-23T10:15:04Z",
    "finishedAt": "2026-11-23T10:17:22Z",
    "outcome": "succeeded"
  },
  "tasks": [
    {
      "key": "Ld /Foo.app/Foo normal arm64",
      "target": "Foo",
      "tool": "ld",
      "ran": true,
      "duration": "0.42s",
      "exitCode": 0,
      "inputs":  ["path/a.o", "path/b.o"],
      "outputs": ["Foo.app/Foo"],
      "executionReason": "signature-mismatch",
      "signatureMismatchDepKeys": ["Compile path/a.swift"],
      "errors": [],
      "warnings": []
    }
  ]
}
```

An optional Graphviz `.dot` renderer takes the same report and emits
the DAG with ran / skipped / failed tasks color-coded.

Both outputs come from the same internal `Report` struct — adding
another sink (NDJSON, protobuf for a BE push) is a renderer swap, not
a re-parse.

## Execution reasons — how we classify

v2 classifies with a single snapshot — the current build's `build.db`. B.2
extends this with a prior snapshot to detect per-rule signature changes and
promote "ran but has no prior rule" into a distinct bucket. The follow-up
review pass introduced `re-evaluated-cached` to split llbuild's "I looked
at this rule" from "I invoked its command". The **six reason values** the
classifier now emits:

| Condition | Reason |
|---|---|
| No `rule_results` row for the task's `C`-prefixed manifest key | `cold` |
| Rule's `built_at` == `info.iteration` AND activity log has a section (`Ran == true`) | `ran-this-build` |
| Rule's `built_at` == `info.iteration` AND activity log has no section (`Ran == false`) | `re-evaluated-cached` |
| Rule's `built_at` == `info.iteration` AND `PriorRules != nil` AND no prior rule for this key | `new-task` |
| Rule's `built_at` < `info.iteration` | `skipped-up-to-date` |
| Rule's `built_at` > `info.iteration` (shouldn't happen, defensive) | `unclassified` |

The `re-evaluated-cached` split matters on warm builds where llbuild
re-evaluates hundreds of rules per build but only a handful actually invoke
work. Treating "rule iteration matches current" as "ran" overcounted ~4x
on IceCubes — `ran-this-build` reads 50 but only 13 commands actually
executed per the activity log.

When `info.iteration` is zero (test fixtures / older schema), v2's fallback
to the activity-log Ran flag as the tiebreaker still applies so warm-build
output stays usable.

**Signature-change detection (B.2):** orthogonal to the bucket. When
`Snapshot.PriorRules != nil` and a matched task's prior rule carries a
different serialized Value BLOB than the current, `Task.SignatureChanged =
true`, `Task.PriorValueHex` / `Task.CurrentValueHex` hold short sha256
fingerprints of the respective Value blobs, and `Task.PriorSignature`
still carries the prior key-identity hash for backwards compatibility.
Note that the raw `signature` column on `rule_results` is a stable
key-identity hash — it does NOT change when a rule's output flips, so we
diff on Value bytes. The UI should render `prior=<CurrentValueHex>
now=<PriorValueHex>`, not `prior=<PriorSignature> now=<Signature>`.

**Upstream-dependency attribution — landed in v4** (see below). The CLI now
walks the current snapshot's adjacency to attribute every ran-this-build
task to its minimal upstream-ran causes and surfaces the root-cause set.
This doesn't replace Phase B — on a single snapshot it attributes "which
other re-ran task fed this one", not "whose signature changed first". That
answer still needs the prior build's `build.db`.

**File-change surface (`Report.FileChanges`)**: when a prior snapshot is
available, Classify walks each ran Command's direct Node (file) inputs and
emits `FileChange{NodeKey, DownstreamTaskKey}` entries for Nodes whose
stored Value BLOB differs from the prior snapshot. This answers "which
source file changed and what consumed it" directly; without this surface,
the Command-level sig-diff collapses every change into whatever happens to
sit atop llbuild's chain (usually `ClangStatCache`).

## Known limitations / carve-outs

- **Head-of-chain rules excluded from `RootCauseKeys`.** `ClangStatCache`
  (header stat cache, refreshed every build), llbuild's
  `stale-file-removal` and `build-headers-stale-file-removal` fire on
  every build regardless of source edits. Treating them as root causes
  surfaces them for every single compile that consumes their output. The
  `isHeadOfChain` helper in `reason.go` filters them from the aggregate
  `RootCauseKeys` list and from `ChangeCauseKeys`. Task-level `RootCause`
  is still populated (they remain root causes from the raw dep-walk's
  perspective) — only the Report-level aggregate drops them. Extend the
  helper when a new per-build-fired rule is identified.
- **Gate rules walked through transparently.** llbuild inserts phony
  aggregation `Gate target-X:all` rules between targets. They have no
  underlying work and are a dead end for an operator. `ChangeCauseKeys`
  recurses one hop through each direct-upstream Gate (bounded at 3 hops)
  to surface its real Command upstream instead.
- **`ChangeCauseKeys` falls through to nearest ran ancestor.** When the
  direct one-hop walk yields no ran Commands but the transitive walk
  shows some exist, Classify walks further upstream and surfaces the
  frontier of ran ancestors. This keeps the field consistent with
  `UpstreamRanCount` — previously `UpstreamRanCount > 0` could coincide
  with an empty `ChangeCauseKeys`. Walked keys share the same flat list
  shape as direct keys; the field docstring notes keys may be direct OR
  the nearest transitive ran-ancestor.

## Phasing

| Phase | Scope | Status |
|---|---|---|
| **v0** | xcactivitylog → flat list of tasks w/ status, duration, errors. JSON dump. No manifest. No build.db. | **landed** |
| **v1** | + manifest parse → real planned task graph joined to executed sections. Graphviz dot renderer. | **landed** |
| **v1.1** | Join-time fixups on top of v1: Task.Args populated from the matched manifest Command; description-side normalization widened (`\ ` → ` `, whitespace-run collapse) on both sides of the join so SwiftDriver/CompileSwift rows land; SwiftCompile/SwiftEmitModule/SwiftDriverJobDiscovery activity-log sub-events attribute to the parent `SwiftDriver Compilation <target>` command by target name; llbuild `P<N>:<scope>:<config>:` priority prefix split off the keys and surfaced as `Task.Priority` + `Task.ManifestKey`, with Dependency edges and dot labels using the stripped form. | **landed** |
| **v2** | + build.db read (`modernc.org/sqlite`, pure Go) → execution-reason classifier populates `Task.ExecutionReason` / `Task.Iteration` / `Task.Signature`. Four buckets: `cold`, `ran-this-build`, `skipped-up-to-date`, `unclassified`. Pretty output gains a one-letter reason column (C/R/S/U); dot renderer thickens node borders for `ran-this-build`. | **landed** |
| **v2.1** | `Task.Iteration int64` replaces the earlier `Task.BuiltAt time.Time` carrier — the llbuild `built_at` column is an iteration counter, not a wall-clock timestamp, so the time.Time rendering (`1970-01-01T00:00:01Z` for iter=1) was actively misleading. `RuleResult.Iteration` now typed accordingly; classification semantics unchanged. | **landed** |
| **v3** | + `Task.Diagnostics []Diagnostic` populated by `AttachDiagnostics(report, derivedData, DiagMode)` — stderr-regex extractor surfaces `path:line:col: severity: message` records from `Task.StderrExcerpt` (clang + swiftc form), continuation lines fold into the preceding record's message. `cmd xcode analyze --diagnostics=auto\|dia\|stderr\|off` (default `auto`). Pretty output gains a one-letter `[E/W/N]` diagnostic column + indented per-diagnostic continuation lines. | **landed (stderr only; .dia deferred — see below)** |
| **v4** | + Upstream attribution via the `rule_results.dependencies` BLOB (NOT a separate `rule_dependencies` table — see the build.db notes above). `Snapshot.Deps map[string][]string` carries the full adjacency after Decoder resolves each int64 key_id back to its `key_names` string. `attribution.go` adds `TransitiveCommandDeps` / `DirectCommandDeps` / `UpstreamRanCommands` that walk the `C -> N -> C` two-hop so Command-to-Command edges are first-class. Classify now populates `Task.UpstreamRanCount`, `Task.RootCause`, `Task.ChangeCauseKeys` (truncated at 10 entries + `…` sentinel) and the Report-level `RootCauseKeys` sorted list. Pretty output gains a `*` prefix for root causes and a `U=N` upstream-ran-count column. | **landed** |
| **B.1** | Snapshot primitive. `buildanalyzer.SnapshotBuildDB(derivedData, storeRoot)` copies the live DB via SQLite `VACUUM INTO` under `<storeRoot>/<build-hex>/<iter>-<unix>.db`, pruning to the 10 newest per build-hex. `DefaultSnapshotRoot()` resolves `~/.local/state/xcelerate/build-analyzer/snapshots` (overridable via `BITRISE_BUILD_ANALYZER_SNAPSHOT_ROOT`). New CLI: `bitrise-build-cache xcode snapshot-builddb --derived-data <path> [--snapshot-root <dir>]`. Soft-fails to stderr on missing DB. | **landed** |
| **B.2** | Historical-snapshot classification. `PickSnapshotForLog` picks the newest snapshot <= the log's finish-time plus the next-older as prior. `ReadSnapshotPair` populates `Snapshot.PriorRules`. `Classify` extends to set `Task.SignatureChanged` + `Task.PriorSignature` on diff, and promotes a ran-this-build task with no prior rule to `ReasonNewTask`. `cmd xcode analyze` auto-picks the matching snapshot when `--derived-data` is set; `--snapshot-root` overrides the store root, `--no-snapshot` forces the live DB (debug). Pretty output gains `N` in the reason column and `*` trailing marker on sig-changed tasks. | **landed** |
| **B.2.1** | Attribution + sig-diff review pass: `ReasonReEvaluatedCached` (`V` in the reason column) splits llbuild-re-checked-but-not-executed from actually-ran; `Task.PriorValueHex` / `CurrentValueHex` carry 16-hex sha256 fingerprints of the rule Value blobs so the UI can render honest diffs (the key-identity `PriorSignature` is stable per rule key and misled the previous display); `Report.FileChanges` surfaces changed input file Nodes attributed to their downstream Command; head-of-chain rules (`ClangStatCache` + stale-file-removal) are filtered from `RootCauseKeys` and gated through in `ChangeCauseKeys`; `ChangeCauseKeys` recurses through phony `Gate …` rules and falls through to the nearest-ran-ancestor frontier when the direct one-hop walk yields nothing but transitive ran ancestors exist. | **landed** |
| **B.3** | Orchestration — auto-trigger snapshot on build completion. Deferred: the wrapperless-flow proxy already watches DerivedData for other reasons and is the right place to hook this without a second fsnotify. | **deferred** |

### v3 fallback — `.dia` reader deferred

Per the v3 scope policy (ship stderr-only when the `.dia` bitstream decoder
blows the budget), v3 lands with the stderr path only. The deferral rationale:

- `.dia` is clang's LLVM bitstream encoding — a VBR6 stream with a block
  hierarchy (META / DIAG) and ABBREV records. Writing a tractable pure-Go
  reader means implementing the bitstream VBR codec, abbreviation definitions,
  the clang-serialized-diagnostics schema (record kinds 1–7 for version,
  diagnostic, source-range, fix-it, filename, category, …), and a string pool.
  Minimum viable reader is a 1–2 day job on its own; a robust one is longer.
- Swift's `swiftc` doesn't emit `.dia` at all. The stderr path is the sole
  source for Swift diagnostics regardless of what we do with `.dia`.
- The stderr extractor covers the high-value case — a human-legible
  `path:line:col: error: message` is what a build-log reader cares about —
  and the richer ranges / fix-its / notes that `.dia` carries are marginal
  for post-mortem analysis without an IDE to surface them.

The DiagMode enum (`auto`, `dia`, `stderr`, `off`) and `AttachDiagnostics`
signature are stable: a future PR drops a `dia.go` reader in and the `dia` /
`auto` branches switch from no-op to populated without touching the CLI flag
or the Diagnostic struct.

#### Known gap: empty `StderrExcerpt` on today's SLF parse

The v0 SLF walker lifts the longest string of each section as `commandDetail`
and currently leaves `StderrExcerpt` empty — the activity log DOES embed the
compiler's stderr in `emittedOutputText` but the heuristic lift doesn't
attribute it to the stderr field. Result: on a real failing MinimalApp build,
`AttachDiagnostics` sees empty stderr on every task and surfaces zero
records. The structured diagnostic extraction is covered by a synthesized
integration test (`TestAttachDiagnostics_Auto_UsesStderrPath`) that injects
the swiftc-shaped blob the SLF parser SHOULD surface. A later phase that
walks the SLF schema properly (v3.1 or v4) unlocks this end-to-end on real
builds; the stderr extractor itself needs no change.

Each phase is its own PR. v0 ships useful on day one (standalone JSON
of executed tasks, already better than `xcodebuild -verbose` greps).

## Placement in the repo

- `internal/xcelerate/buildanalyzer/` — new package. Pure data +
  readers, no network, no auth.
  - `manifest.go` — JSON decoder for `manifest.json` (**v1**).
  - `ruledb.go` — `modernc.org/sqlite` reader for `build.db` (**v2**,
    pure Go, no cgo). v4 extended this to decode the
    `rule_results.dependencies` BLOB into `Snapshot.Deps` (a
    `map[string][]string` adjacency keyed by rule key).
  - `reason.go` — v2 execution-reason classifier against the ruledb
    Snapshot. v4 extended with attribution: `Task.UpstreamRanCount`,
    `Task.RootCause`, `Task.ChangeCauseKeys`, `Report.RootCauseKeys`.
  - `attribution.go` — v4. Iterative DFS over `Snapshot.Deps` with a
    Command-level visited set; `C -> N -> C` two-hop so Node (file)
    intermediates are stepped over and callers only see Command keys.
  - `snapshotstore.go` — B.1. `VACUUM INTO`-based snapshot primitive +
    on-disk layout under `<storeRoot>/<build-hex>/<iter>-<unix>.db`,
    prune cap, `DefaultSnapshotRoot()`, `ListSnapshots()`.
  - `historical.go` — B.2. `PickSnapshotForLog(...)` picks current + prior
    from the store given the log's finish-time; `ReadSnapshotPair(...)`
    opens both and populates `Snapshot.PriorRules` so Classify runs the
    signature-diff + new-task promotion path.
  - `activity.go` — SLF walker that surfaces one Task per
    `IDEActivityLogCommandInvocationSection`, plus v1's `JoinManifest`
    that annotates Tasks with Planned/Tool/Inputs/Outputs and emits
    `Dependency` edges from the manifest's input/output file-path join.
  - `dot.go` — pure stdlib Graphviz `.dot` renderer (v1); nodes
    color-coded green/red/grey, with v2's `penwidth` bump for
    `ran-this-build` borders.
  - `task.go` — the joined `Report` struct + `Task` + `Dependency`.
  - `diagnostic.go` — `Diagnostic` / `Range` / `FixIt` structs + severity
    constants used by v3's stderr extractor and the deferred `.dia` reader.
  - `stderr_diag.go` — `ExtractStderrDiagnostics` regex-based extractor for
    `path:line:col: severity: message` clang / swiftc headers with
    continuation-line folding (**v3**).
- `cmd/xcode/analyze.go` — cobra subcommand `bitrise-build-cache
  xcode analyze <DerivedDataPath|xcresult>`. Auto-picks the matching
  snapshot under `DefaultSnapshotRoot()` when `--derived-data` is set;
  `--snapshot-root` / `--no-snapshot` override the flow (B.2).
- `cmd/xcode/snapshot_builddb.go` — B.1 snapshot subcommand.
- `docs/xcode-build-analyzer.md` — this file.

Pure post-mortem. No overlap with the live CAS/proxy path; the
analyzer is an off-line consumer of artifacts the OS already wrote.

## Tradeoffs considered

- **SLF parser**: hand-rolled Go (~300 LOC, spec reverse-engineered)
  vs shelling out to `xclogparser` (Swift dep). Picked pure Go for
  distribution — the CLI ships a single static binary and every added
  external dep bloats the install matrix.
- **SQLite driver**: `modernc.org/sqlite` (pure Go, no cgo) vs
  `mattn/go-sqlite3` (cgo, faster). Pure Go wins on cross-compile
  cleanliness; the DB is tiny (KB–MB) and post-mortem, speed is a
  non-issue.
- **Manifest parser**: `encoding/json` works out of the box. The design
  doc assumed YAML — current Xcode emits JSON (see the deviation note
  above). No third-party dep needed.
- **llbuild signature semantics**: v1 treats the signature as opaque
  ("mismatched" or "matched"). Reverse-engineering llbuild's signature
  composition is a project unto itself and would only improve the
  "why" message, not the "what". Deferred.

## How it compares to existing coverage

The CAS/analytics pipeline (proxy + enricher + wrapperless parity work
tracked under ACI-310) reports **aggregate per-invocation** numbers —
cache hit rate, bytes transferred, hit counts, build-op metrics,
optional xcresult summary. Those answer "did caching help on this
build, and did the build succeed".

This analyzer answers an orthogonal question: "which *tasks* ran, how
long did they take, how do they depend on each other, and why did the
re-ran ones re-run". None of that is derivable from CAS stats or
xcresult.

Both streams eventually feed Insights, and a BE roll-up can join them
by Xcode's build UUID + DerivedData path.

## References

- `internal/xcactivitylog/reader.go` — existing SLF entry point; the
  analyzer extends it.
- `internal/xcelerate/wrapperless/` — the CAS stats pipeline the
  analyzer is explicitly NOT touching.
- [XCLogParser](https://github.com/MobileNativeFoundation/XCLogParser) —
  reference SLF parser (Swift) used to cross-check our reader.
- [swift-build / llbuild manifest](https://github.com/swiftlang/swift-build)
  — source of truth for the `manifest-*.xcbuild` schema.
