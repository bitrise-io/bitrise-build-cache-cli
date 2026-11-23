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

Path: `~/Library/Developer/Xcode/DerivedData/<Proj>-<hash>/Build/Intermediates.noindex/XCBuildData/<build-hex>.xcbuilddata/build.db`

SQLite database. Source of "did this task's signature change since the
last build". Key tables:

- `key_names` — interned string pool for rule keys.
- `rule_results` — one row per llbuild rule: `(key_id, value_bytes,
  built_at, signature)`. `value_bytes` is an opaque llbuild blob;
  `signature` is the hash llbuild compares against to decide "re-run
  or reuse".
- `rule_dependencies` — adjacency: `(rule_id, dependency_rule_id)`.

v1 uses this purely as a "was the signature the same as last build?"
oracle per key. v2 may walk `rule_dependencies` to attribute the
mismatch to a specific upstream key.

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

For each task present in the manifest:

| Condition | Reason |
|---|---|
| No `rule_results` row for its signature in prior build.db | `cold` |
| Signature matches prior row + no corresponding xcactivitylog section | `skipped-up-to-date` |
| Signature matches prior row + xcactivitylog section present | `re-ran-same-signature` *(rare — forced re-run)* |
| Signature differs from prior row | `signature-mismatch` → list deps whose signature changed |
| Never seen in any prior build.db | `new-task` |

v1 uses llbuild's opaque signature as the mismatch granularity ("this
signature changed"). v2 walks `rule_dependencies` to point at the
single upstream key whose signature changed first.

## Phasing

| Phase | Scope | Status |
|---|---|---|
| **v0** | xcactivitylog → flat list of tasks w/ status, duration, errors. JSON dump. No manifest. No build.db. | **landed** |
| **v1** | + manifest parse → real planned task graph joined to executed sections. Graphviz dot renderer. | **landed** |
| **v2** | + build.db signature diff → execution reasons populated per task. | planned |
| **v3** | + `.dia` parsing via cgo libclang (OR skip permanently if coverage from xcactivitylog stderr is enough). | TBD |

Each phase is its own PR. v0 ships useful on day one (standalone JSON
of executed tasks, already better than `xcodebuild -verbose` greps).

## Placement in the repo

- `internal/xcelerate/buildanalyzer/` — new package. Pure data +
  readers, no network, no auth.
  - `manifest.go` — JSON decoder for `manifest.json` (**v1**).
  - `ruledb.go` — `modernc.org/sqlite` reader for `build.db` (no cgo,
    v2).
  - `activity.go` — SLF walker that surfaces one Task per
    `IDEActivityLogCommandInvocationSection`, plus v1's `JoinManifest`
    that annotates Tasks with Planned/Tool/Inputs/Outputs and emits
    `Dependency` edges from the manifest's input/output file-path join.
  - `dot.go` — pure stdlib Graphviz `.dot` renderer (v1); nodes
    color-coded green/red/grey.
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
