# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
make check              # Full check: go-generate → lint-fix → test-unit
make test-unit          # Run all unit tests: go test -tags unit -race ./...
make lint               # Run golangci-lint v2
make lint-fix           # Run golangci-lint with auto-fix
make protoc             # Regenerate protobuf code
make govulncheck        # Run vulnerability scanner
make run-xcelerate-proxy # Run xcelerate proxy locally
```

**Running a single test:**
```bash
go test -tags unit -race ./cmd/gradle -run TestFunctionName
```

The `-tags unit` flag is required for all test runs.

## Architecture

This is a Go CLI tool (Cobra-based) that configures build cache for Gradle, Bazel, and Xcode on machines where it runs. The binary name is `bitrise-build-cache`.

**Entry point:** `main.go` imports command packages via blank imports (`_`), which register their Cobra subcommands via `init()` functions. `cmd/common` contains the root command and shared utilities (HTTP client, logging).

**Command structure:** `cmd/{gradle,bazel,xcode}/` — each package has `activate_*.go` (main entry point for configuring cache) and supporting commands (save/restore, enable-for, etc.).

**Internal packages:**
- `internal/config/{gradle,bazel,xcelerate,common}/` — configuration generation and file modification for each build system
- `internal/xcelerate/` — Xcode compilation caching implementation (proxy server, derived data handling, Xcode arg parsing, analytics)
- `internal/build_cache/kv/` — key-value storage client for the build cache protocol
- `internal/hash/` — blake3 hashing utilities
- `internal/stringmerge/` — merges content into config files using `# [start]/[end] generated-by-bitrise-build-cache` marker blocks

**Protocol Buffers:** `proto/` contains definitions for Bazel remote execution API, KV storage, and LLVM CAS/session protocols. Regenerate with `make protoc`.

**Authentication:** Uses `BITRISE_BUILD_CACHE_AUTH_TOKEN` and `BITRISE_BUILD_CACHE_WORKSPACE_ID` env vars (auto-configured on Bitrise CI).

## Testing Patterns

- Tests use `testify` (assert/require/mock)
- Each command package has a `setup_test.go` that initializes mock loggers
- Tests use dependency injection for mockable components

### Mock generation with moq

This project uses [moq](https://github.com/matryer/moq) for generating mocks. The binary is at `~/.asdf/installs/golang/1.25.4/bin/moq` (v0.6.0). Run `go generate ./...` to regenerate, or invoke moq directly.

**Important:** Generate mocks as `_test.go` files in the same package to avoid import cycles:
```
//go:generate moq -stub -out client_mock_test.go -pkg ccache . Client
```
This keeps the mock test-only (not importable) while allowing internal (`package ccache`) test files to use it without a cycle. Do **not** put mocks in a `mocks/` subdirectory for interfaces that are tested from within the same package.

The `-stub` flag makes uncalled methods return zero values instead of panicking.

### cmd/reactnative builder pattern

`cmd/reactnative/activate_react_native.go` uses exported builder functions for testability:
- `BuildGradleActivationFn(activateFn)` — wraps gradle activation, expands `~/.gradle`, sets cache enabled/push enabled
- `BuildXcodeActivationFn(activateFn)` — wraps Xcode activation, sets `DebugLogging`
- `BuildCppActivationFn(activateFn)` — wraps ccache activation with `ccacheconfig.DefaultParams()`
- `BuildStartStorageHelperFn(executableFn, startProcessFn)` — starts the storage helper as a detached process (survives activation)

Production `var defaultXxxFn` wires each builder to the real implementation. Tests pass fakes to the builders.

### internal/ccache package

The ccache IPC proxy (`internal/ccache/`) implements a binary protocol between ccache and the Bitrise build cache:
- `requestProcessor` handles one request per connection: GET (0x00), PUT (0x01), REMOVE (0x02), STOP (0x03), SetInvocationID (0xB1)
- `IpcServer` uses `sync.Once` for lazy `getCapabilities` and a semaphore for concurrency control
- `LoggerFactory func(invocationID string) (log.Logger, error)` creates per-invocation loggers
- `processResultOutcome` constants: `PROCESS_REQUEST_OK`, `PROCESS_REQUEST_MISS`, `PROCESS_REQUEST_SHOULD_STOP`, `PROCESS_REQUEST_ERROR`, `PROCESS_REQUEST_PUSH_DISABLED`
- `sessionState` uses `atomic.Int64` for hit/miss/byte counters
- `keyToPath` supports layouts: `""` or `"flat"` (hex), `"subdirs"` (`ccache/1-xx/rest`), `"bazel"` (`ac/<64hex>`)

#### Important log lines asserted by CI

The `cache-ccache-test` workflow in `gradle-plugins/bitrise.yml` asserts on the following log lines written to `~/.local/state/ccache/logs/ccache-<id>.log`. **Do not change their text without updating the assertions.**

| Log line (exact pattern) | Level | File | What it means |
|---|---|---|---|
| `Server listening on` | Info | `ipc_server.go:Run` | Storage helper started and is accepting connections |
| `[SetInvocationID] parent=` | Debug | `request_processor.go:handleSetInvocationID` | Invocation ID handshake succeeded (requires `--debug` flag) |
| `Server shutting down` | Info | `ipc_server.go:Run` | Storage helper stopped cleanly |
| `[Get - <hex>] OK took` | Debug | `request_processor.go:logCallStats` | A ccache GET hit (remote cache hit) |

These are annotated with `// CI: asserted by cache-ccache-test workflow` in the source.

## Benchmark Phasing

Benchmark phasing allows measuring build performance with and without cache. The phase is queried from the Bitrise API during activation and affects both Gradle and Xcode builds.

**API:** `GET /build-cache/{workspaceID}/invocations/{buildTool}/command_benchmark_status` (in `internal/config/common/benchmark.go`). The `buildTool` is `gradle` or `xcode`. Query params identify the build: `app_slug` + `workflow_name` (Bitrise CI) or `external_app_id` + `external_workflow_name` (external CI).

**Phases:**
- **baseline** — cache is disabled, analytics-only mode. Measures build time without cache.
- **warmup** — cache is enabled but may not be fully populated yet. Logs a warning about potentially suboptimal performance.

**Storage:** The phase is persisted in two ways during activation:
1. `BITRISE_BUILD_CACHE_BENCHMARK_PHASE` env var (exported via envman / GITHUB_ENV / shell RC files)
2. `~/.local/state/xcelerate/benchmark/benchmark-phase.json` (file fallback)

The file I/O is in `internal/config/common/benchmark.go` (`WriteBenchmarkPhaseFile` / `ReadBenchmarkPhaseFile`), shared by both Gradle and Xcode.

**Architecture:** `BenchmarkPhaseProvider` (interface in `internal/config/common/benchmark.go`) is injected into both `TemplateInventory()` (gradle) and `NewConfig()` (xcode). The client is created at the command layer and passed in. The provider is only called when `metadata.CIProvider != ""` (i.e., on CI). Passing `nil` skips the check entirely.

**Gradle flow:** `ApplyBenchmarkPhase()` in `internal/config/gradle/benchmark.go` processes the phase result, exports the env var, writes the file, and overrides `ActivateGradleParams` (disables cache on baseline). Called from `TemplateInventory()` in `activate_for_gradle_params.go`.

**Xcode flow:** `ApplyBenchmarkPhase()` in `internal/config/xcelerate/benchmark.go` processes the phase result, exports the env var, writes the file, and disables `BuildCacheEnabled` on baseline. Called from `NewConfig()` in `config.go`. The xcodebuild wrapper (`cmd/xcode/xcodebuild.go`) reads the phase from the env var (with file fallback) and includes it in the analytics invocation via `CacheConfigMetadata.BenchmarkPhase`.

**Note:** The benchmark phase is intentionally NOT stored in the xcelerate config file (`~/.bitrise-xcelerate/config.json`) — only in the env var and the benchmark phase file, matching the Gradle approach.

## Go Version

Keep the `go` directive in `go.mod` at **1.24**. Do not bump it to 1.25 or later. This is a hard requirement imposed by the step libraries that depend on this CLI — they need Go 1.24 compatibility. When running `go mod tidy` or updating dependencies, pin any transitive packages that would require Go 1.25+ (typically `golang.org/x/{net,sys,text,tools}` and `google.golang.org/genproto`) to versions that are compatible with Go 1.24.

## Bitrise Workflow Scripts

Inline script steps directly in the `bitrise.yml` / `bitrise_rn_config/bitrise.yml` files when the content is short. Extract to a file under `scripts/` only when the script body exceeds ~10 lines.

## Linting

This project uses golangci-lint v2. Notable rules to follow when generating code:

- **nlreturn**: blank line required before `return` statements (and other block-terminating statements) when preceded by other code. Always add an empty line before `return`, `continue`, `break` in non-trivial blocks.
- **noctx**: use `DialContext` / `CommandContext` / etc. instead of context-free variants (`net.DialTimeout`, `exec.Command`). For intentionally detached processes (e.g. background helpers that must outlive the parent), suppress with `//nolint:noctx // intentionally detached: <reason>`.
- **lostcancel**: always `defer cancel()` immediately after `context.WithCancel` / `WithTimeout` / `WithDeadline`.

## Release Process

See the `/release` skill (`.claude/skills/release/SKILL.md`) for the full end-to-end release process. It covers both gradle-plugin-triggered releases and CLI-only releases.
