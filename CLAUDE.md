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
Create a GitHub release for the step. This one **can** be marked as "latest". **Version numbering:** The step version can be a minor bump (e.g., 1.5.0 → 1.6.0) when the underlying plugin has new features. Check the latest existing release tag.

### 5. Merge steplib PR
After the step release CI completes, a PR appears in `https://github.com/bitrise-io/bitrise-steplib`. It usually needs a rebase. Approve and enable auto-merge. Always wait for CI to pass.

### Flaky E2E tests — cache hit rate
The `features-e2e` pipeline includes cache hit rate assertions. If a test (e.g., `feature-e2e-gradle-7`) fails with `cacheHitRate: want != 0, got 0`, it's likely because cache items were evicted or because of co-located caches across data centers (builds may land on a different DC than the one with the warm cache). **Keep rebuilding the failed workflows** — it may take 2-3 attempts. This is not a real failure.

### Key Bitrise App IDs
| App | Bitrise App ID |
|-----|---------------|
| bitrise-build-cache-cli | `1a2ddc0a-bab0-4db1-9b78-4c13aae180ba` |
| Step unified CI | `48fa8fbee698622c` |
