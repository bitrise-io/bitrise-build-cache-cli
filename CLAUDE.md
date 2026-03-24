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

## Linting

This project uses golangci-lint v2. Notable rules to follow when generating code:

- **nlreturn**: blank line required before `return` statements (and other block-terminating statements) when preceded by other code. Always add an empty line before `return`, `continue`, `break` in non-trivial blocks.
- **noctx**: use `DialContext` / `CommandContext` / etc. instead of context-free variants (`net.DialTimeout`, `exec.Command`). For intentionally detached processes (e.g. background helpers that must outlive the parent), suppress with `//nolint:noctx // intentionally detached: <reason>`.
- **lostcancel**: always `defer cancel()` immediately after `context.WithCancel` / `WithTimeout` / `WithDeadline`.

## Release Process

A CLI release can be triggered by two scenarios:
1. **Dependency update:** An `update_plugins` workflow creates a PR with updated Gradle plugin versions (triggered automatically from the gradle-plugins publish pipeline, or manually).
2. **Code changes:** Direct code changes to the CLI itself, merged via a normal PR.

**IMPORTANT: When the user asks to do a release, drive the ENTIRE process end-to-end in a single conversation.** Use the Bitrise MCP server to monitor build statuses (poll every 30s), abort irrelevant workflows, and guide the user through each step. Do not stop and wait for the user between steps — proactively monitor, report status, and move to the next step as soon as the previous one completes.

### 1. Merge the PR
For dependency updates: the `update_plugins` workflow creates a PR in this repo. For code changes: merge the feature/fix PR normally. In both cases, monitor the CI pipeline. If there are flaky cache hit rate failures, rebuild them. Once all checks pass, approve with `gh pr review --approve` and enable auto-merge with `gh pr merge --merge --auto`. **NEVER use `--admin` to bypass checks — always wait for CI to go green.**

### 2. Create CLI GitHub release
Create a GitHub release. **Do NOT mark it as "latest"** — another CI job handles that. Follow the format of existing releases for release notes.

**Version numbering — always ask the user** which semver bump to apply (patch, minor, or major). Use these guidelines as defaults:
- **Patch** bump: dependency-only updates (e.g., plugin version bumps) or bug fixes
- **Minor** bump: new features or non-breaking behavioral changes in the CLI
- **Major** bump: breaking changes

Check the latest existing release tag to determine the next version.

### 3. Wait for step auto-update PR
The CLI release triggers an auto-update workflow in `https://github.com/bitrise-steplib/bitrise-step-activate-gradle-remote-cache`. The unified Bitrise CI app for steps is `48fa8fbee698622c`. The PR title will be "feat: Release new CLI". Monitor CI, then approve with `gh pr review --approve` and enable auto-merge with `gh pr merge --merge --auto`.

### 4. Create step GitHub release
Create a GitHub release for the step. This one **can** be marked as "latest". **Version numbering: the step version bump should match the CLI version bump.** If the CLI release was a patch bump, the step should also be a patch bump. If the CLI release was a minor bump, the step should be a minor bump. Check the latest existing release tag.

### 5. Merge steplib PR
After the step release CI completes, a PR appears in `https://github.com/bitrise-io/bitrise-steplib`. It usually needs a rebase. Approve and enable auto-merge. Always wait for CI to pass.

### Flaky E2E tests — cache hit rate
The `features-e2e` pipeline includes cache hit rate assertions. If a test (e.g., `feature-e2e-gradle-7`) fails with `cacheHitRate: want != 0, got 0`, it's likely because cache items were evicted or because of co-located caches across data centers (builds may land on a different DC than the one with the warm cache). **Keep rebuilding the failed workflows** — it may take 2-3 attempts. This is not a real failure.

### Key Bitrise App IDs
| App | Bitrise App ID |
|-----|---------------|
| bitrise-build-cache-cli | `1a2ddc0a-bab0-4db1-9b78-4c13aae180ba` |
| Step unified CI | `48fa8fbee698622c` |
