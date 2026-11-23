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
- `rule_dependencies` — adjacency: `(rule_id, dependency_rule_id)`. v2
  does not read this; v3 may walk it to attribute the mismatch to a
  specific upstream key.

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

### 4. `*.dia` — clang serialized diagnostics (v3, optional)

Path: `Build/Intermediates.noindex/.../ArchIntermediates/.../*.dia`

Clang's binary diagnostic format. Richer than the stderr block (ranges,
fix-its, notes). Parsing requires cgo libclang; v1 skips, v2 revisits.

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

v2 (landed) classifies with a single snapshot — the current build's `build.db`
— so the signature-mismatch vs new-task distinction is **deliberately
deferred** (that needs the prior snapshot). The four buckets v2 ships:

| Condition | Reason |
|---|---|
| No `rule_results` row for the task's `C`-prefixed manifest key | `cold` |
| Rule's `built_at` == `info.iteration` | `ran-this-build` |
| Rule's `built_at` < `info.iteration` | `skipped-up-to-date` |
| Rule's `built_at` > `info.iteration` (shouldn't happen, defensive) | `unclassified` |

When `info.iteration` is zero (test fixtures / older schema), v2 falls back
to the activity-log Ran flag as the tiebreaker so warm-build output stays
usable.

**Deferred for later phases (both would need a second data source):**

- `signature-mismatch` vs `new-task` — needs the prior `build.db` to diff
  the signature column. v2 exposes the per-task `signature` hex so a BE
  roll-up CAN run the diff across invocations, even though the CLI itself
  doesn't.
- Upstream-dependency attribution (the "which input key changed first"
  message) — needs `rule_dependencies` traversal.

## Phasing

| Phase | Scope | Status |
|---|---|---|
| **v0** | xcactivitylog → flat list of tasks w/ status, duration, errors. JSON dump. No manifest. No build.db. | **landed** |
| **v1** | + manifest parse → real planned task graph joined to executed sections. Graphviz dot renderer. | **landed** |
| **v1.1** | Join-time fixups on top of v1: Task.Args populated from the matched manifest Command; description-side normalization widened (`\ ` → ` `, whitespace-run collapse) on both sides of the join so SwiftDriver/CompileSwift rows land; SwiftCompile/SwiftEmitModule/SwiftDriverJobDiscovery activity-log sub-events attribute to the parent `SwiftDriver Compilation <target>` command by target name; llbuild `P<N>:<scope>:<config>:` priority prefix split off the keys and surfaced as `Task.Priority` + `Task.ManifestKey`, with Dependency edges and dot labels using the stripped form. | **landed** |
| **v2** | + build.db read (`modernc.org/sqlite`, pure Go) → execution-reason classifier populates `Task.ExecutionReason` / `Task.BuiltAt` / `Task.Signature`. Four buckets: `cold`, `ran-this-build`, `skipped-up-to-date`, `unclassified`. Pretty output gains a one-letter reason column (C/R/S/U); dot renderer thickens node borders for `ran-this-build`. | **landed** |
| **v3** | + `.dia` parsing via cgo libclang (OR skip permanently if coverage from xcactivitylog stderr is enough). | TBD |

Each phase is its own PR. v0 ships useful on day one (standalone JSON
of executed tasks, already better than `xcodebuild -verbose` greps).

## Placement in the repo

- `internal/xcelerate/buildanalyzer/` — new package. Pure data +
  readers, no network, no auth.
  - `manifest.go` — JSON decoder for `manifest.json` (**v1**).
  - `ruledb.go` — `modernc.org/sqlite` reader for `build.db` (**v2**,
    pure Go, no cgo).
  - `reason.go` — v2 execution-reason classifier against the ruledb
    Snapshot.
  - `activity.go` — SLF walker that surfaces one Task per
    `IDEActivityLogCommandInvocationSection`, plus v1's `JoinManifest`
    that annotates Tasks with Planned/Tool/Inputs/Outputs and emits
    `Dependency` edges from the manifest's input/output file-path join.
  - `dot.go` — pure stdlib Graphviz `.dot` renderer (v1); nodes
    color-coded green/red/grey, with v2's `penwidth` bump for
    `ran-this-build` borders.
  - `task.go` — the joined `Report` struct + `Task` + `Dependency`.
- `cmd/xcode/analyze.go` — cobra subcommand `bitrise-build-cache
  xcode analyze <DerivedDataPath|xcresult>`.
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
