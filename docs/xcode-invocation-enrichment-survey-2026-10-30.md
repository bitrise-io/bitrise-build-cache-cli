# Xcode Invocation Enrichment Survey (2026-10-30)

Research-only. Surveys what a build-time tool wrapping `xcodebuild` (and sitting
alongside Xcode.app for the wrapperless IDE flow) can harvest, grouped by source,
with pointers to reference implementations we can mine. No implementation plan.

## TL;DR — what to steal first

1. **Per-target `fetchedFromCache` + `compilationEndTimestamp` / `compilationDuration` split from `.xcactivitylog`** — XCMetrics derives both. Lets us say "linking blocked for 2 min after compile finished" and "4/23 targets never ran because cache fetched product" — neither is in our current payload.
2. **Build-step detail-type histogram** (`CompileSwift`/`CompileC`/`Ld`/`PhaseScriptExecution`/`CopySwiftLibs`/`CompileAssetCatalog`/`CompileStoryboard`/`CodeSign`) with total ms per bucket — single `-showBuildTimingSummary` scrape; one page of code; makes "cache helped but PhaseScriptExecution still dominates" visible.
3. **Swift type-check / expression-type-check tails** (`-Xfrontend -debug-time-expression-type-checking`) → top-N offenders with file+line+ms. Already extractable from `.xcactivitylog` (XCLogParser does it) and from stderr. High value, zero schema stress, fits as an attachment-shaped array.
4. **`-ftime-trace` + ClangBuildAnalyzer-style aggregation** for Objective-C/C++ heavy codebases. The `.json` traces are per-TU chrome-trace blobs; our proxy already sees `CompileC` invocations, so we can conditionally scrape when the flag is on without demanding user action.
5. **Host-side snapshot per invocation** — thermal pressure (`pmset -g therm`), free RAM, uptime, swap-in rate, VM presence (`system_profiler`/`sysctl`). XCMetrics stores this as a sibling table `build_hosts`; we only emit `HwCPUCores`+`HwMemSize`+`Datacenter`.

Everything else in the report is secondary — these five give us ~80% of what XCMetrics-grade reporting looks like with ~20% of their collection surface.

---

## What we already emit (baseline)

Grep base: `internal/xcelerate/analytics/types.go`, `internal/blobstats/blobstats.go`, `internal/xcelerate/xcresult/parser.go`, `internal/xcelerate/enrichment/manifest.go`.

**`analytics.Invocation` (top-level row written per build):**
- Identity: `invocationId`, `invocationDate`, `bitriseOrgSlug/appSlug/buildSlug/stepId`, `externalApp/Build/Workflow` for off-Bitrise CI, `providerId`, `cliVersion`, `commitHash`, `commitEmail`, `branch`, `repositoryUrl`, `workflowName`.
- Host: `hostname`, `username`, `os`, `hwCpuCores`, `hwMemSize`, `datacenter`, `defaultCharset`, `locale`.
- Build: `command`, `fullCommand`, `durationMs`, `success`, `error`, `xcodeVersion`, `toolBuildNumber`, `benchmarkPhase`.
- Cache: `hitRate`, `cacheBlobStats` (full `blobstats.Snapshot` — per-direction latency/size histograms, throughput p10/p50/p90 with retained samples, errorCount, missCount, skippedAlreadySaved, CAS vs KV breakdown).
- Env: raw `envs` map (whitelist-filtered on BE side).
- `targets[]` + `failures[]` — populated from `xcrun xcresulttool get build-results` on the wrapper self-enrich path only (`internal/xcelerate/xcresult/parser.go`). Targets carry `name` + `buildDurationInMs`; failures carry `targetName` + first-line `message`.

**`CacheOperation` (per-op, orthogonal stream):** operationType, started/duration, transferSize, cacheKey, cacheKeyType, error, repoURL, commit, branch, workflow, `fileStats{filesToTransfer, filesTransferred, filesFailed, filesMissing, totalFiles}`.

**Discovery sources we already touch:**
- `~/Library/Developer/Xcode/DerivedData/*/Logs/Build/LogStoreManifest.plist` — walked by `enrichment/manifest.go`; we read `uniqueIdentifier`, `className`, `title`, `signature`, `schemeIdentifier-schemeName`, `fileName`, `highLevelStatus`, `timeStartedRecording`/`timeStoppedRecording`. **We do not open the `.xcactivitylog` files the manifest points to.** That's the single biggest gap.
- `xcrun xcresulttool get build-results --format json` — only on the wrapper path, only when the `.xcresult` fits under 200 MB, only for target names + per-target duration + first failure line.

What follows is everything a public tool extracts beyond that.

---

## Reference projects

### Spotify XCMetrics (https://github.com/spotify/XCMetrics)

License: **Apache-2.0** (per source-file headers; `gh api .../license` reports `NOASSERTION` because the LICENSE file is non-standard, but every .swift file is Apache-2 licensed to the ASF). Shippable to vendor fragments or port to Go.

Architecture: Swift Vapor backend (Fluent ORM) + Swift Mac client + Backstage plugin UI. Client watches DerivedData for new xcactivitylogs, parses them in-process via **XCLogParser** (Spotify-authored, below), batches metrics + host snapshot + custom plugin metadata, POSTs to the backend. Backend is Vapor, Postgres-backed, 14 tables all partitioned by day (`PartitionedByDay`).

**Exact schema** (from `Sources/XCMetricsBackendLib/Common/Models/*.swift`):

**`builds`** — one row per invocation:
```
id (string), project_name, machine_name, schema,
start_timestamp, end_timestamp,
start_timestamp_microseconds, end_timestamp_microseconds,  // double, sub-ms precision
duration (double), build_status, warning_count, error_count,
tag, is_ci, user_id, user_id_256, category,
compiled_count,                                            // int: total steps that actually compiled
was_suspended (bool),                                      // true if the build was paused/resumed
compilation_end_timestamp, compilation_duration            // last-compile wall-clock vs link-end
```

**`build_targets`** — one per target per build:
```
id, build_identifier, name, start/end, duration,
warning_count, error_count,
fetched_from_cache (bool),                                 // key signal: cache hit at target level
category, compiled_count,
compilation_end_timestamp, compilation_duration            // same split as build-level
```

**`build_steps`** — one per compile/link/copy/script step:
```
id, build_identifier, target_identifier,
title, signature, type,                                    // type = DetailStepType enum
architecture, document_url,
start/end, duration,
warning_count, error_count,
fetched_from_cache
```
`type` is XCLogParser's `DetailStepType`: `cCompilation`, `swiftCompilation`, `scriptExecution`, `createStaticLibrary`, `linker`, `copySwiftLibs`, `compileAssetsCatalog`, `compileStoryboard`, `writeAuxiliaryFile`, `linkStoryboards`, `copyResourceFile`, `mergeSwiftModule`, `XIBCompilation`, `swiftAggregatedCompilation`, `precompileBridgingHeader`, `validateEmbeddedBinary`, `validate`, `other`, `none`.

**`build_errors` / `build_warnings`** (and `build_notes`):
```
build_identifier, parent_identifier, parent_type,
title, type, document_url,
severity, starting_line, ending_line, starting_column, ending_column,
character_range_start, character_range_end,
detail, clang_flag (warnings only)
```

**`build_hosts`** — one per build, hardware/thermal snapshot:
```
host_os, host_architecture, host_model,
host_os_family, host_os_version,
cpu_model, cpu_count, cpu_speed_ghz,
memory_total_mb, memory_free_mb,
swap_total_mb, swap_free_mb,
uptime_seconds, timezone, is_virtual
```

**`swift_functions` + `swift_type_checks`** — top-N compile-time offenders:
```
file, (signature,) starting_line, starting_column,
duration (ms), occurrences
```

**`xcode_versions`** — `version` + `build_number` per build.

**`build_metadata`** — JSON-blob extension point for custom plugins (git branch, PR number, thermal throttle flag, etc.).

Collection method: XCMetrics' client parses the raw `.xcactivitylog` via XCLogParser's Swift library and never shells out to `xcresulttool`. All target/step/warning/error data comes from the compressed binary log, so it works for both `xcodebuild` CLI builds and Xcode IDE builds, as long as Xcode wrote the log.

Xcode-drift notes:
- The schema was authored against Xcode 11-era SLF (v8-10). Spotify maintainers merged SLF-11 support (Xcode 15.3 attachments) via XCLogParser; XCMetrics itself did not have to change.
- No reported break on Xcode 16; the schema is additive. On Xcode 26 the problem is "the enricher gets no CAS metrics", not "the log is unparseable" — see our own note at `project_xcode_27_log_parser_gap` in the user memory.

### XCLogParser (https://github.com/MobileNativeFoundation/XCLogParser)

License: **Apache-2.0**. Spotify-authored, now MobileNativeFoundation-hosted. **This is the reference implementation of SLF parsing.** Swift SPM package, macOS and Linux (via Docker).

Reports (six reporters):
1. `json` — full hierarchical BuildStep tree with substeps, warnings, errors, notes, swiftFunctionTimes, swiftTypeCheckTimes, clangTimeTraceFile ref, linkerStatistics.
2. `flatJson` — same but flattened for columnar insert (what XCMetrics backend uses).
3. `summaryJson` — single top-level summary object.
4. `chromeTracer` — chrome://tracing timeline.
5. `issues` — just errors+warnings.
6. `html` — standalone interactive report.

Also: `dump` (raw SLF → JSON), `manifest` (LogStoreManifest.plist → JSON), post-build Xcode-scheme auto-parsing.

**Public `BuildStep` schema** (`Sources/XCLogParser/parser/BuildStep.swift`, trimmed):
```
type (main|target|detail)
machineName, buildIdentifier, identifier, parentIdentifier
domain, title, signature, schema
startDate, endDate (ISO-8601 strings)
startTimestamp, endTimestamp, duration (double, unix epoch)
detailStepType (enum above), buildStatus
subSteps[], warningCount, errorCount, architecture, documentURL
warnings[Notice], errors[Notice], notes[Notice]
swiftFunctionTimes[SwiftFunctionTime]
swiftTypeCheckTimes[SwiftTypeCheck]
fetchedFromCache (bool)
compilationEndTimestamp, compilationDuration
clangTimeTraceFile (path to .json if -ftime-trace enabled)
linkerStatistics (LinkerStatistics, if -Xlinker -print_statistics enabled)
```

**`LinkerStatistics`** (ld64 `-print_statistics`):
```
totalMS,
optionParsingMS/Percent, objectFileProcessingMS/Percent,
resolveSymbolsMS/Percent, buildAtomListMS/Percent,
runPassesMS/Percent, writeOutputMS/Percent,
pageins, pageouts, faults,
objectFiles, objectFilesBytes,
archiveFiles, archiveFilesBytes,
dylibFiles, wroteOutputFileBytes
```

**SLF format** (`docs/Xcactivitylog Format.md`):
- `.xcactivitylog` = gzip wrapper over `SLF0`-prefixed binary stream.
- Eight typed tokens: `#` int64, `^` little-endian hex double, `"` UTF-16-counted string, `(` array of class instances, `%` class-name registration, `@` class-instance ref by index, `-` null, `*` JSON blob.
- Key classes: `IDEActivityLogSection`, `IDEActivityLogMessage`, `DVTDocumentLocation`, `IDEActivityLogCommandInvocationSection`.
- **Gotcha:** SLF only marks class-instance start, not end or property names — parsers must hard-code property ordering from Apple's private `IDEFoundation.framework` headers. Reverse-engineered; **version-fragile**.
- SLF versions: 8 → 10 → 11. v11 (Xcode 15.3+) adds per-section attachment arrays carrying JSON payloads for task metrics / build operation data. **This is where the new CAS/compilation-cache metrics now live for Xcode 15.3+.** Our own gap note (`project_xcode_27_log_parser_gap`) is the Xcode 27 attachment-schema delta.

### xcbeautify (https://github.com/cpisciotta/xcbeautify)

License: **MIT**. Swift CLI, maintained successor to xcpretty for modern xcodebuild output.

Structured output beyond prettification:
- **CI-native** workflow commands: GitHub Actions (`::warning file=...::`, `::error`), TeamCity (`##teamcity[...]`), Azure Pipelines (`##vso[...]`).
- Parses modern-build-system output + SPM.

Not usable as a pure library — line-based regex on `xcodebuild` stderr/stdout, no public API we'd want to depend on. Worth as pattern reference for "classify a build line" if we ever want to annotate wrapped stderr. **Not a data source itself.**

### XCResultKit (https://github.com/davidahouse/XCResultKit)

License (per repo page): **not asserted** (`NOASSERTION`). Swift SPM package; macOS-only (shells to `xcresulttool` + `xccov`).

Build-side entities (test entities omitted — out of scope):
- `XCResultFile` — entry point, takes a URL to the bundle.
- `ActionsInvocationRecord` — top-level: list of `ActionRecord`, `metadataRef`, `metrics` (summary counts).
- `ActionRecord` — per-action (build / archive).
- `ActionResult` → `issuesRef`, `logRef`, `diagnosticsRef`.

Xcode 16+ `--legacy` deprecation: in Xcode 16, `xcrun xcresulttool get --format json` without `--legacy` returns a completely restructured JSON. XCResultKit (as of mid-2025) still passes `--legacy` to keep its decoders working. The new mode has first-class subcommands: `get build-results`, `get log`, `metadata get`, `export attachments`. **This is the path forward for us** — `internal/xcelerate/xcresult` already uses `get build-results`, so we're on the modern path.

### Nike Clima / Uber Build Experience / Airbnb Buck tooling

- No public open-source Xcode telemetry collector from any of them as of 2026-10. Uber's "Scaling iOS at Uber with a complex build system" blog (unverified direct URL; 404 at time of this report) describes Buck adoption but doesn't publish the collector. Nike Clima is a Swift Xcode-build-runner, not telemetry.
- **Emerge Tools** (https://www.emergetools.com) is the only commercial player in this exact slot — SaaS, not open-source; measures binary size, startup, build time, flagged perf issues. Collection source isn't public but is clearly xcactivitylog + xcresult + their own in-app profiler. Worth watching as a UX reference; nothing to vendor.

### Buildkite / CircleCI / GitHub Actions orbs

Pure runner wiring (`xcodebuild | xcbeautify`). No native instrumentation beyond what xcbeautify emits as CI workflow commands. Nothing to steal.

### Swift Package Manager / xcbuild telemetry

- SwiftPM: nothing public. `swift build --vv` emits plan JSON; no first-class metrics path.
- Apple open-sourced `swift-build` (XCBuild) in 2024 as part of Swift Build, with llbuild as the engine. **No telemetry hooks** exposed, but the manifest-*.xcbuild format and build.db come from here, so they're inspectable (see Data Sources below).

---

## Data sources (independent of any one project)

### 1. `.xcactivitylog` (SLF binary, in DerivedData/Logs/Build/)

**Location:** `~/Library/Developer/Xcode/DerivedData/<project-id>/Logs/Build/<uuid>.xcactivitylog`, indexed by sibling `LogStoreManifest.plist`.

**Format:** gzip + SLF0 header + tagged value stream. See XCLogParser's `docs/Xcactivitylog Format.md`. Not an Apple-documented format; schema reverse-engineered from `IDEFoundation.framework` private headers.

**What it carries (and what XCLogParser extracts):**
- Full task tree: main → target → step, with signature/title/domain/documentURL.
- Per-step start/end wall-clock timestamps (double, sub-ms microsecond precision).
- Per-step `fetchedFromCache` boolean (**this is the signal our current payload misses most**).
- Per-step warnings/errors/notes with full source location (file URL, line/column, character range, severity, detail body, clang flag).
- Swift function-body timings (`-Xfrontend -debug-time-function-bodies`): file, line/col, signature, durationMs, occurrences.
- Swift expression-type-check timings (`-Xfrontend -debug-time-expression-type-checking`): file, line/col, durationMs, occurrences.
- Linker statistics (when `-Xlinker -print_statistics` passed): ld64 phase breakdown with page I/O counts.
- Clang `-ftime-trace` file paths (XCLogParser surfaces the path; the JSON itself is a separate file to open).
- Build status string per step.
- Architecture per step.
- SLF v11+ attachments (Xcode 15.3+): per-section JSON payloads carrying task metrics and build-operation data — **this is where Apple's new compilation-cache metrics live** (`CompilationCacheMetrics` on Xcode 26+; see `project_xcode_27_log_parser_gap`).

**Parser options for us:**
- Vendor XCLogParser as a sidecar binary (Apache-2, Swift, we'd ship the macOS binary alongside the CLI). ~1 MB. Already battle-tested on billions of builds (Spotify).
- Rewrite in Go. Non-trivial — SLF class-table hardcoding is the bulk of the work. ~1-2 weeks for a parser that handles v8-v11, plus ongoing drift maintenance each Xcode release.
- Shell out to `xclogparser parse --reporter flatJson --file <log>` and ingest its JSON. Lowest effort.

Risk: SLF schema drifts with every ~2 Xcode major versions. XCLogParser's track record is "merged a PR within a release cycle" each time, but adopters absorb a ~2-week window where the parser lags Xcode. Mitigate by pinning XCLogParser version and failing-open (empty attachments) rather than failing-closed.

### 2. `build.db` (SQLite, in DerivedData/Build/Intermediates.noindex/XCBuildData/)

**Location:** `~/Library/Developer/Xcode/DerivedData/<project-id>/Build/Intermediates.noindex/XCBuildData/build.db`

**Format:** SQLite 3, written by llbuild (`swift-llbuild`) which Xcode uses as its build engine.

**What's inside** (reverse-engineered; Apple-undocumented; schema confirmed by inspecting a live file and cross-referencing swift-llbuild sources at https://github.com/swiftlang/swift-llbuild):
- `info` — single-row key/value: schema version, llbuild version.
- `key_names` — rowid → canonical key string (file paths, task names, virtual nodes).
- `rule_results` — rowid, key_id, value (blob — the computed result signature), built_at (iteration), computed_at, dependencies (serialized ids).
- `rule_dependencies` — many-to-many between rule results and the inputs they consumed.

**Usable signals:**
- Count of invalidated-vs-reused rules per build = incremental hit-rate at the task-graph level.
- Which keys were invalidated and why (via `dependencies` chain) = "null incremental build took 30s because key X changed".
- Historical trend of rule count = project growth proxy.

**Caveats:**
- No official API; schema can and does change (last known break: XCBuild → Swift Build rename in 2024, which left llbuild schema stable but Xcode's writer version-bumped it).
- Must be read with no concurrent writer — hold off during an active build.
- No existing OSS Go parser; a trivial `database/sql` + `go-sqlite3` connect would work. **Not high effort.**

**References:**
- Open-source llbuild writer: https://github.com/swiftlang/swift-llbuild (SQLite schema in `lib/BuildSystem/BuildDatabase.cpp`).
- The pspdfkit/nutrient blog (`build-performance-tracking-in-ios-projects`, now redirects to nutrient.io) covers early schema exploration. **Unverified at fetch time** — URL redirected; content described above is from the swift-llbuild source itself.

### 3. `manifest-*.xcbuild` (llbuild YAML / binary plist)

**Location:** `~/Library/Developer/Xcode/DerivedData/<project-id>/Build/Intermediates.noindex/XCBuildData/<uuid>-manifest.xcbuild`

**Format:** llbuild build description. Historically a YAML file; current Xcode writes it as a binary plist of the same conceptual shape (nodes + commands + tools + targets). The llbuild BuildSystem format (https://github.com/swiftlang/swift-llbuild/blob/main/docs/buildsystem.rst) is the authoritative schema reference.

**Carries:**
- Full task graph: every command Xcode planned to run, with inputs/outputs, tool class, environment, dependencies.
- Target-to-command mapping.
- Virtual nodes (phony dependencies, aggregation points).

**Usable signals:**
- "The build planned 1,247 CompileSwift commands and 12 Ld commands" — pre-execution upper bound on work.
- "The CompileSwift command for File.swift expected input Foo.h which doesn't exist" — misconfiguration detection.
- Diff manifest-N vs manifest-N+1 → "why did this build take longer than the last".

**Risk:** binary plist format, no stable Apple-documented schema; same drift risk as SLF.

### 4. `BuildDescription-*.xcbuilddata` + `BuildDescriptionCacheIndex-*.xcbuilddata`

**Location:** `~/Library/Developer/Xcode/DerivedData/<project-id>/Build/Intermediates.noindex/XCBuildData/`

Companion to the manifest; Xcode's cached "pre-compiled" project description. Mostly opaque; sometimes grows unbounded on project corruption. **Signal we could emit:** size + mtime, as a cheap "project description reindex happened" marker. Nothing deeper is usable without reversing Xcode's private format.

### 5. `.xcresult` bundles

Already consumed on the wrapper path (`internal/xcelerate/xcresult/parser.go`). Via `xcrun xcresulttool get` — build-relevant Xcode 16+ subcommands only (test subcommands out of scope):
- `get build-results --format json` → actions, destinationTargets, targets, runs, errors, warnings, metrics (buildDurationInMs). **Currently used.**
- `get log` → raw action/console log.
- `metadata get` → bundle format version, creator tool, arch, etc.

**Gaps vs what we extract:** `metadata get` for bundle-format drift detection; `get log` as a fallback when `.xcactivitylog` is absent (Xcode 26 CLI without `-resultBundlePath`).

Modern format replaces the pre-Xcode-16 `--legacy` tree-of-objects with a flat set of subcommand outputs. Our parser is already on the modern path — no deprecation blast radius for us.

### 6. Swiftc internal timing (`-stats-output-dir <dir>` + `-driver-time-compilation`)

**Mechanism:** pass `OTHER_SWIFT_FLAGS="-stats-output-dir /tmp/swift-stats"` to Xcode; swift-driver + each swift-frontend writes `stats-<timestamp>-<name>-<random>.json` into the dir. Format: LLVM's `PrintStatisticsJSON` output — a flat dict of counter name → number. Counter names like `AST.NumSourceBuffers`, `AST.NumSourceLines`, `AST.NumLoadedModules`, `SILGen.NumSILGenFunctions`, `IRGen.NumIRGlobalsEmitted`, `time.swift.Parsing.wall`, `time.swift-frontend.<pass>.wall`.

Also:
- `-driver-time-compilation` — per-job user/sys/wall time, printed to stderr. Human-readable; parseable.
- `-debug-time-function-bodies` → stderr "N.NNms\t<file:line:col>\t<function>" lines. **XCLogParser already extracts.**
- `-debug-time-expression-type-checking` → same shape for expressions. **XCLogParser already extracts.**

**Aggregation:** Apple ships `utils/process-stats-dir.py` in the Swift repo. For our use, directly ingest the per-TU JSON stats and emit a top-N or histogram per build.

**License:** emitted data is yours; the tooling (Swift project) is Apache-2.

**Risk:** stats files pile up — must be rotated. Per-TU dict with ~200 counters each; thousands of files per build.

### 7. Clang `-ftime-trace` + ClangBuildAnalyzer

**Mechanism:** pass `OTHER_CFLAGS="-ftime-trace"` and clang emits `<objfile>.json` next to each `.o`. Format is **Chrome Tracing** (same as `chrome://tracing`) — array of events with `ph` (phase), `name`, `ts`, `dur`, `pid`, `tid`, `args`. Categories: `Source` (file parsed), `Instantiating template`, `OptModule`, `OptFunction`, `CodeGen Function`, `Backend`, `Frontend`, `Include file`.

**Aggregation:** ClangBuildAnalyzer (https://github.com/aras-p/ClangBuildAnalyzer, **license: Unlicense** — public-domain-equivalent, safe to vendor). Walks all `-ftime-trace` JSONs modified in a session and emits:
- Slowest files to parse (lexer+parser frontend time).
- Slowest files to codegen (LLVM backend time).
- Slowest templates to instantiate, with counts + averages.
- Slowest functions to compile/optimize.
- Expensive headers with inclusion counts + full include chains.

**Does not cover:** linking, LTO.

**For us:** XCLogParser already exposes the `clangTimeTraceFile` path per step. Pipeline: XCLogParser → path list → ClangBuildAnalyzer-style aggregator (or vendor the binary) → attach rolled-up Top-10 to the invocation payload.

### 8. `xcodebuild -showBuildTimingSummary`

Appends an aggregated section at the end of a build log:
```
Build Timing Summary

CompileSwift (123 tasks) | 428.1 seconds
CompileC (56 tasks) | 102.3 seconds
Ld (12 tasks) | 54.2 seconds
PhaseScriptExecution (18 tasks) | 44.5 seconds
CodeSign (12 tasks) | 11.0 seconds
...
```
**No JSON mode** — regex-scrape stderr. Dirt-cheap to add; gives us the step-type histogram the TL;DR calls out.

### 9. `xcodebuild -list -json`, `-showBuildSettings -json`, `-showdestinations`

Already used in `internal/xcelerate/xcodebuildinfo/`. More fields we don't persist:
- `-showBuildSettings -json`: every effective build setting resolved for a scheme — ENABLE_BITCODE, SWIFT_OPTIMIZATION_LEVEL, DEBUG_INFORMATION_FORMAT, DEPLOYMENT_TARGET, SWIFT_COMPILATION_MODE (`wholemodule` vs `singlefile`), ARCHS, VALID_ARCHS, GCC_OPTIMIZATION_LEVEL, SWIFT_WHOLE_MODULE_OPTIMIZATION. **Cheap config-drift signal to attach once per invocation.**
- `-list -json`: schemes/targets/configurations — mostly identity, duplicates what we already carry.

### 10. Instruments / `.trace` bundles

`xctrace record` can run `--template 'Time Profiler'` on a build. Produces a `.trace` bundle. Parsable only by Instruments app or `xctrace export`; the exported format is a verbose binary. **Not worth parsing headlessly** — too expensive for a per-build enrichment path, better left as an opt-in developer tool.

### 11. `swift-symbolgraph-extract`, `swift-doc`

Produces symbol graph JSON per module. Interesting for codebase-size trending (number of public symbols, deprecation count) but not build-perf signal. **Skip for now.**

### 12. DVTAnalytics (private Xcode telemetry)

Xcode internally sends anonymous telemetry to Apple when developer analytics opt-in is enabled. Written to `~/Library/Logs/DVTAnalytics/*.plist`. Fields include build successes/failures, project kind, user actions. **Not stable, Apple-private, don't ship a parser.** Mentioned only for completeness.

### 13. System-side signals (worth folding in)

All runnable without sudo during a user build:
- `pmset -g therm` → CPU-speed limit, CPU speed-limit cause, scheduler/available. Human-readable table; cheap regex scrape. **Does not require sudo** per `pmset(1)` — only writes do.
- `pmset -g rawbatt` → battery raw state on laptops.
- `sysctl -a | grep hw.` → `hw.ncpu`, `hw.physicalcpu`, `hw.perflevel0.physicalcpu`/`perflevel1.physicalcpu` (P-core vs E-core counts on Apple Silicon), `hw.cpufrequency_max`, `hw.cachelinesize`.
- `sysctl kern.boottime` → uptime seconds.
- `sysctl vm.loadavg` → 1/5/15-minute load averages.
- `vm_stat` → page-ins/page-outs/compressed/swapped — detect memory pressure during the build.
- `powermetrics --samplers thermal,cpu_power,gpu_power -i 1000 -n 1` → thermal pressure + per-cluster watts. **Requires sudo** — unusable without password prompt.
- `system_profiler SPHardwareDataType -json` → model identifier (MacBookPro18,2 etc.), serial, chip, cores, memory. Slow (~1s), run once per session not per build.
- `uptime` → bootup time + load average, no privilege required.
- `mdutil -s /` → Spotlight indexing status per volume. Detect "indexing in progress" bursts that eat IO during a build.
- `df -k /path/to/DerivedData` + walking `du -sh ~/Library/Developer/Xcode/DerivedData/<project>` → derived-data size trend (cheap: just `stat` the top-level dir and sum).
- `tmutil status` → Time Machine backup in progress (steals IO).
- `csrutil status` + `nvram -p | grep boot-args` → SIP state and arboot flags (one-time, config-drift only).
- Virtualization detection: `sysctl -n kern.hv_vmm_present` returns 1 on a VM.

Equivalent to XCMetrics' `build_hosts` row (`host_os`, `cpu_count`, `cpu_speed_ghz`, `memory_total/free_mb`, `swap_total/free_mb`, `uptime_seconds`, `is_virtual`). We emit `hwCpuCores`, `hwMemSize` only — thermal, swap, uptime, free RAM, VM-flag are the obvious additions.

---

## Candidate enrichments for our invocation payload

Legend: effort S/M/L; value H/M/L; risk L/M/H.

| Source | Metric | Effort | Value | Risk | Notes |
|---|---|---|---|---|---|
| `.xcactivitylog` | Per-target `fetchedFromCache` bool + `compilationDuration` vs `duration` split | M | H | M | Needs XCLogParser (vendored binary or Go port). Biggest single signal we're missing. |
| `.xcactivitylog` | Per-step `DetailStepType` + duration histogram per build | M | H | M | Same ingest path as above. Enables "cache helps on CompileSwift but PhaseScriptExecution dominates". |
| `-showBuildTimingSummary` | Aggregated per-task-type ms (no JSON, regex scrape) | S | H | L | Cheap stop-gap for the above; one regex over stderr. |
| swiftc `-debug-time-expression-type-checking` | Top-N slowest expressions file:line:ms:occurrences | M | H | M | Flag user-opt-in or auto-inject on debug builds. XCLogParser extracts for us. |
| swiftc `-debug-time-function-bodies` | Top-N slowest functions file:line:signature:ms | M | H | M | Same ingest. |
| clang `-ftime-trace` + aggregator | Top expensive headers, templates, slowest TUs | L | H | M | Needs `-ftime-trace` enabled in CFLAGS. ClangBuildAnalyzer (Unlicense) is the aggregator. ObjC/C++-heavy codebases only. |
| `-Xlinker -print_statistics` | ld64 phase breakdown + page I/O | M | M | L | Only when flag is enabled; useful for diagnosing linker-dominated builds (large monoliths). XCLogParser already surfaces. |
| `.xcactivitylog` | Full warning/error list with file+line+severity+clangFlag | M | M | M | Overlaps our 1-line `failures[]`. Expand to warnings + source location = better diagnostics; grows payload. Consider separate stream. |
| `.xcactivitylog` attachments (SLF v11+) | Xcode's own `CompilationCacheMetrics` / task-metrics JSON per step | M | H | H | Xcode 15.3+ attach these per section; Xcode 26 added CompilationCacheMetrics specifically — our enricher misses it (`project_xcode_27_log_parser_gap`). High value because it's Apple's view of our cache. Risk: schema drift each release. |
| `xcresulttool metadata get` | Bundle format version + creator tool | S | L | L | Drift detection for xcresult format itself. |
| `xcodebuild -showBuildSettings -json` | Scheme-effective SWIFT_OPTIMIZATION_LEVEL, SWIFT_COMPILATION_MODE, DEPLOYMENT_TARGET, ENABLE_BITCODE, DEBUG_INFORMATION_FORMAT | S | H | L | One row per invocation; detects "release build with -Onone" misconfig. Cheap. |
| `build.db` (llbuild SQLite) | Rule-reuse rate + invalidation reasons for incremental builds | M | M | M | Needs `database/sql` + llbuild schema. No OSS Go parser today — write our own, ~100 LOC. Risk: Xcode writer version bumps. |
| manifest-*.xcbuild | Planned-task count per build | M | L | M | Low immediate value; expensive to parse (binary plist). Defer. |
| swiftc `-stats-output-dir` | Per-build counter rollups (AST size, SILGen/IRGen timings) | L | M | M | Thousands of per-TU JSON files; rotation + aggregation overhead. Only enable opt-in. |
| `pmset -g therm` | CPU speed-limit at start/end of build (thermal throttling) | S | H | L | One shell-out at build start + end. XCMetrics' host-telemetry equivalent. No sudo. |
| `sysctl vm.loadavg` + `vm_stat` | Load average, free/compressed/swapped pages at build-start | S | M | L | Context for slow builds; cheap. |
| `sysctl kern.boottime` | Uptime seconds | S | L | L | Trivial. |
| `sysctl -n kern.hv_vmm_present` | VM-presence flag | S | L | L | Trivial; XCMetrics has `is_virtual`. |
| `system_profiler SPHardwareDataType -json` | Model identifier, chip, physical cores, serial | S | M | L | ~1s cost; run once per session, cache. |
| `mdutil -s /` | Spotlight indexing-in-progress during build | S | M | L | Explains "slow despite warm cache" runs. |
| `tmutil status` | Time Machine backup active during build | S | L | L | Rare but worth one bit. |
| `du` on DerivedData root | Size at build-end (growth trend) | S | L | L | Trend signal; near-zero cost. |
| `-Xfrontend -driver-time-compilation` | Per-job driver timing from stderr | S | L | L | Mostly duplicates XCLogParser. Skip unless we avoid parsing SLF. |
| `.xcactivitylog` | SLF-v11 attachment: build-operation JSON (whole-build metrics) | M | M | H | Apple-added, undocumented, moves each release. |
| Instruments `.trace` | Full CPU time profile of the build | L | L | H | Overkill; opt-in dev tool, not telemetry. |
| DVTAnalytics plists | Xcode's own anonymous telemetry | L | L | H | Apple-private, don't ship. |

---

## Cross-cutting gotchas

- **SLF schema drift**: ~every 2 Xcode majors. XCLogParser is the canary — track their issues/PRs. Our existing Xcode 27 attachment gap (`project_xcode_27_log_parser_gap`) is the latest instance.
- **`xcresulttool --legacy` deprecation**: Xcode 16 deprecated the legacy flag; XCResultKit still passes it. We already use the modern `get build-results` subcommand, so we're fine.
- **Private frameworks**: SLF class ordering depends on `IDEFoundation.framework` internals. Vendoring XCLogParser transitively depends on Apple not reordering class properties mid-release. XCLogParser handles this by maintaining per-SLF-version parsers; adopt the same discipline.
- **License compatibility**: XCMetrics backend is Apache-2 (safe for us to port/vendor fragments). XCLogParser is Apache-2 (safe). ClangBuildAnalyzer is Unlicense (safe). XCResultKit is NOASSERTION — **do not vendor as-is**; re-implement or ask upstream to add a license. xcbeautify/xcpretty are MIT (safe).
- **Payload size**: XCMetrics writes to 14 tables because `builds` and `build_steps` have very different cardinality (1 vs thousands). We currently cram everything into one `Invocation` row. For step-level data we need a sibling stream (`CacheOperation`-style) rather than inflating `Invocation`.
- **Collection on CI vs local**: pmset/vm_stat/sysctl answers are different on a dedicated CI VM vs a developer laptop — the former is uninteresting (fixed hardware, no thermals), the latter is highly explanatory of flakiness. Design the collector to no-op on known CI hostnames to avoid noise.
- **Enricher ancestry gate**: our enricher already rejects some IDE sidecars (`project_enricher_ide_ancestry_rejection` note). Any new source we add needs to work for both the wrapper path and the wrapperless/IDE path, or the IDE will be invisible to the new signal.
- **Benchmark phase**: all comparisons must respect `benchmarkPhase=baseline` vs `warmup` vs steady-state. XCMetrics does not segment by this; we must.
- **Xcode 26 CLI loses `.xcactivitylog` without `-resultBundlePath`** — our own note (`project_xcode_26_cli_no_xcactivitylog`). The wrapper auto-injects; wrapperless must either inject the flag or fall back to attachment-only collection via the IDE's own log path.

---

## Open questions / gaps

1. **Build-identity correlation on wrapperless flow**: XCMetrics' `buildIdentifier` is the xcactivitylog UUID. Our wrapperless flow derives InvocationID from PID+start-ns (per `project_xcode_build_identity_derivation_2026_10_06`). We need a one-to-one map between the two so that per-step rows from the log can be stitched to our invocation row. The LogStoreManifest `uniqueIdentifier` field is the key — we already read it; wire it into the enriched invocation.
2. **Where to persist step-level rows**: current `Invocation` row carries only aggregate counts. A sibling stream (akin to `CacheOperation`) scales better and matches XCMetrics' decomposition. BE schema decision needed before client work.
3. **Opt-in vs always-on for compiler flags**: `-debug-time-function-bodies`, `-debug-time-expression-type-checking`, `-ftime-trace`, `-stats-output-dir`, `-Xlinker -print_statistics` all require customer action. Decide: do we inject them silently (risk: stderr noise, build-time overhead), gate on `benchmarkPhase`, or document opt-in?
4. **Vendoring vs porting XCLogParser**: shipping a Swift binary alongside a Go CLI is new ground; porting the SLF parser to Go is a 1-2 week spike with ongoing drift burden. Decision shapes everything else.
5. **Payload shape vs cost**: XCMetrics writes 14 partitioned-by-day tables because the backend is theirs. Our invocation BE normalises differently (`PutInvocation` is a single row per call) — need BE guidance on whether to multiplex via multiple PUTs, a nested JSON blob, or a separate endpoint.
6. **Does CompilationCacheMetrics in SLF v11+ attachments duplicate what our proxy already sees?** If yes (`CAS cache hit on blob X`), it's redundant with `blobstats`. If no (Apple reports higher-level "this task cache-hit" at the IDE/driver boundary rather than per-blob), it's complementary. Needs a small spike against an Xcode 26+ log to answer.
7. **Any gated access to a BuildDescription-*.xcbuilddata reader?** Apple's open-source Swift Build (released 2024) may now expose read APIs for its own planning data — unverified at time of this report.
