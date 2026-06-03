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

### Three-layer package structure

```
cmd/          → Thin cobra wrappers (flags → params, call pkg/)
pkg/          → Public API structs for external Go consumers (steps)
internal/     → Core business logic, config, protocols
```

**Dependency direction:** `cmd/` → `pkg/` → `internal/`. Never the reverse. This allows external Go packages to import `pkg/` without pulling in cobra or command-level code.

**`cmd/` layer:** Each cobra command maps flags to a params struct and calls the corresponding `pkg/` struct. No business logic — just wiring. `main.go` registers commands via blank imports (`_`) and `init()` functions. `cmd/common` holds the root command and shared globals (`IsDebugLogMode`).

**`pkg/` layer — public API for external consumers:**
- `pkg/ccache/` — `StorageHelper` (Start/Stop/HealthCheck/...), `Activator`, `InvocationRegistry`
- `pkg/reactnative/` — `Activator` (Gradle/Xcode/C++ activation), `Runner` (command execution with analytics)
- Exported structs with public methods, no Go `interface` types at the provider side — consumers define their own interfaces for mocking via Go's implicit satisfaction

**`internal/` layer:**
- `internal/config/{gradle,bazel,xcelerate,common}/` — configuration generation, activation logic, file modification
- `internal/ccache/` — IPC server/client, `Socket` struct for storage helper communication
- `internal/xcelerate/` — Xcode compilation caching (proxy server, derived data, arg parsing, analytics)
- `internal/build_cache/kv/` — key-value storage client for the build cache GRPC protocol
- `internal/hash/` — blake3 hashing utilities
- `internal/stringmerge/` — merges content into config files using `# [start]/[end] generated-by-bitrise-build-cache` marker blocks

**Protocol Buffers:** `proto/` contains definitions for Bazel remote execution API, KV storage, and LLVM CAS/session protocols. Regenerate with `make protoc`.

**Authentication:** Uses `BITRISE_BUILD_CACHE_AUTH_TOKEN` and `BITRISE_BUILD_CACHE_WORKSPACE_ID` env vars (auto-configured on Bitrise CI).

### Patterns for pkg/ structs

- **Exported struct, no interface type:** Export concrete structs with public methods. Consumers define their own interfaces for mocking.
- **Lightweight constructor:** `NewXxx(params)` reads config only — heavy work (GRPC, IPC) happens in methods like `Start()`.
- **DI via exported fields:** For structs that need test injection (e.g. `Activator.Logger`, `Activator.OsProxy`), export the dependency fields. Nil means production default.
- **DI via unexported interface + moq:** For internal details (e.g. analytics hooks), define an unexported interface, implement with a real struct, and use moq to generate test mocks in the internal test package.
- **Section markers:** Use `// Private — ...` comment blocks to visually separate public API from private implementation in files.

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

### GAR mirror of release artifacts (including installer.sh)

The `release` workflow in `bitrise.yml` mirrors three things to a public GAR generic repo (`build-cache-cli-releases` in project `ip-build-cache-prod`, region `us-central1`):

1. The four platform tarballs + the checksums file (consumed by `install/installer.sh`'s `*_URL_GAR` fallback).
2. The pinned `ccache` binaries (consumed by the ccache install flow).
3. **`install/installer.sh` itself**, in two views:
   - `installer.sh:<tag>:installer.sh` — **immutable** pinned copy per release (audit trail; describe-or-upload).
   - `installer.sh:latest-pointer:installer.sh` + `installer.sh:latest-pointer:VERSION` — **mutable** pointers refreshed on every release. The `VERSION` file holds the bare semver and is read by `installer.sh`'s `gar_latest_version()` helper as a tag-resolution fallback when github.com is unreachable. (GAR rejects the literal version_id `latest` as reserved — that's why this view uses `latest-pointer`.)

#### Immutability rule and the documented carve-out

The default rule for this GAR repo is **describe-or-upload, never delete-then-upload** (see `#327` postmortem — race window between delete and upload returns 404 to any consumer hitting the fallback in that window). All binary mirrors and the immutable `installer.sh:<tag>:*` follow this rule.

The **only documented carve-out** is `installer.sh:latest-pointer:*` (installer.sh and VERSION). These are intentionally mutable, refreshed via delete-then-upload on every release. The carve-out is safe here because GAR `latest-pointer` is **only consulted when the primary path (github.com / raw.githubusercontent.com) has already failed** — an already-degraded path, not a hot path that catches the race window during normal operation.

If you add a new artifact, default to the describe-or-upload immutable pattern. Only adopt a mutable `latest-pointer` view if (a) consumers genuinely need "always the newest" without a pin AND (b) the consumer's access pattern is fallback-only, not hot-path.

### R2 mirror of release artifacts (preboot host-VM cache origin)

The `release` workflow also mirrors the four platform tarballs + checksums file to a Cloudflare R2 bucket (`build-cache-cli-releases` in Cloudflare account `a484c7653eeba8c8c00a4bf3967860a3`). Script: `scripts/r2_upload_release_artifacts.sh`. Required Bitrise secrets: `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`.

The R2 bucket is the **origin** for the preboot host-VM cache proxy, served on each build VM at `http://${SUBNET_IP}1:59020/build-cache-cli-releases/<tarball>`. That proxy expects a **flat** layout (object key == filename, no `/<tag>/` prefix), so the R2 script uploads under the bare filename. GAR remains as a versioned public fallback for both `installer.sh` consumers and the startup-script's secondary download path; the two mirrors run in parallel and serve different fallback roles.

Idempotency follows the same describe-or-upload rule (`aws s3api head-object` skip-if-present). R2's S3 API rejects AWS CLI v2's default flexible checksums, so the upload script forces `AWS_REQUEST_CHECKSUM_CALCULATION=when_required` / `AWS_RESPONSE_CHECKSUM_VALIDATION=when_required`.

### Automated preboot startup-script bump

The `bump-prebooting` workflow (chained after `verify-release` in the `release-and-verify` pipeline) opens an auto-merging PR against `bitrise-io/build-prebooting-deployments`, bumping `BITRISE_BUILD_CACHE_CLI_VERSION` and the per-arch sha256 in both startup-script extensions (`preboot-reconciler/startup_script_extension_{linux,macos}_bitvirt.sh`). Only `linux_amd64` and `darwin_arm64` are bumped — those are the only preboot VM architectures.

Script: `scripts/prebooting_pr_bump.sh`. After pushing the branch, the script explicitly merges the PR with `gh pr merge --squash --delete-branch`. The Bitrise Infrabot is a bypass actor on the `production` rule, so the explicit merge clears required-review gates at merge time. GitHub's `--auto` merge mode is intentionally NOT used — auto-merge is a background process that does not honour bypass actors and would block on any required reviewer.

The deployments repo has **no CI on PRs** (no GitHub Actions workflows; the Bitrise GitHub app's check-suites stay `queued` forever with zero check-runs). Any `gh pr checks --watch` would either block on phantom queued suites or exit non-zero on "no checks reported", so the script does not wait for CI. Bypass-merge is the only gate.

After sed-bumping the constants, the script asserts the working tree shows **exactly** `+2/-2` per startup script and no other files modified — defensive check against a loose regex matching unintended lines.

Required Bitrise secret: `PREBOOTING_BOT_TOKEN` (GH PAT for `Bitrise Infrabot`, scoped to `bitrise-io/build-prebooting-deployments`: `contents:write` + `pull_requests:write`). Slack-alerts on failure; safe to retry the workflow without re-cutting the tag.
