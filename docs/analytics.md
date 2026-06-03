# Analytics: Invocation Registration

## Overview

A build run that uses Bitrise Build Cache is reported to the analytics backend as an **invocation**. When several tools cooperate inside a single build, each is its own invocation and the relationships between them are reported as **invocation relations** (parent → child).

Two CLI subcommands act as the public, build-tool-agnostic registration surface — used both by other parts of the CLI (xcode, ccache, react-native wrappers) and by external callers (the bitrise-io/gradle-plugins repo). They live on the root command, not under `ccache` or any tool-specific namespace.

| Command | Flags | What it does |
|---------|-------|-------------|
| `register-invocation` | `--invocation-id` (req), `--build-tool` (req — e.g. `gradle`, `ccache`, `reactnative`, `xcode`) | Registers a single invocation with the analytics backend. |
| `register-child-invocation` | `--parent-id` (req), `--child-id` (req), `--build-tool` (default `ccache`) | Registers a parent → child relation between two invocation IDs. |

Both commands read auth credentials from the multiplatform analytics config (`~/.bitrise/analytics/multiplatform/config.json`), written by `activate react-native`, `activate c++`, or `activate xcode`. This is the single canonical on-disk source of auth credentials — neither the ccache config nor the xcelerate config persists auth.

The backend endpoint used by both commands is `consts.MultiplatformAnalyticsServiceEndpoint` (`https://multiplatform-analytics.services.bitrise.io`).

---

## Conceptual model

```
Wrapper (react-native run, gradle build, xcodebuild wrapper, ...)
   │
   │  register-invocation  →  parent invocation
   │
   ├── register-child-invocation  →  parent → gradle child relation
   ├── register-child-invocation  →  parent → ccache child relation
   └── register-child-invocation  →  parent → xcode child relation
```

`BITRISE_INVOCATION_ID` is the contract carrying the parent ID across processes. A wrapper sets it; child tools read it. If empty, the tool is the top-level invocation and registers itself only.

---

## Use cases

### CLI-internal callers

**`react-native run` (cmd/reactnative + pkg/reactnative).** The wrapper invocation is created inside `react-native run`: it reads `BITRISE_INVOCATION_ID` from the environment, falls back to a fresh UUID if empty, and exports the resolved value back into the child environment. After the build completes, `postRunDeps.run` calls `multiplatform.Client.PutInvocation` directly (Go API, not the CLI subcommand) and then `helper.CollectAndSendStats` registers the parent → ccache child relation. See `docs/reactnative.md` for the full data flow.

**Xcode wrapper (`cmd/xcode/xcodebuild.go`).** When `BITRISE_INVOCATION_ID` is set, `XcodebuildRunner.saveInvocationAndRelation` calls `sendRelation(parentID)` which `PUT`s an `InvocationRelation` to the multiplatform service marking the xcode invocation as a child of the wrapper.

**ccache storage helper (`pkg/ccache/storage_helper.go`).** `StorageHelper.CollectAndSendStats` queries the running storage helper for the (parent, child) pair via IPC `0xB2`, and — when there was activity — calls `InvocationRegistry.RegisterRelation` (which is the same code path the `register-child-invocation` CLI subcommand uses) to register the parent → ccache relation.

### External callers (bitrise-io/gradle-plugins)

**Orchestrator mode — `BitriseCCachePlugin.close()`** (`cache/src/main/kotlin/io/bitrise/gradle/cache/BitriseCCachePlugin.kt`). When Gradle itself is the orchestrator (no wrapper above it), the plugin registers both the parent and the gradle child at the end of the build:

```kotlin
runCli("register-invocation",       "--invocation-id=$parentId", "--build-tool=gradle")
runCli("register-child-invocation", "--parent-id=$parentId", "--child-id=$gradleId", "--build-tool=gradle")
```

`parentId` and `gradleId` here come from build-time parameters. The `register-invocation` call is the gradle plugin's only path for creating a top-level invocation — when a wrapper is present (next case), this call is skipped.

**Wrapper-present mode — `BitriseBuildCacheService.close()`** (`cache/src/main/kotlin/io/bitrise/gradle/cache/BitriseBuildCacheService.kt`). When Gradle runs underneath a wrapper (e.g. `react-native run`, a Bitrise step), the wrapper has already created the parent invocation; the plugin only registers the parent → gradle relation by reading the parent ID from `BITRISE_INVOCATION_ID`:

```kotlin
val parentId = System.getenv("BITRISE_INVOCATION_ID")?.takeIf { it.isNotBlank() }
parentId?.let {
    ProcessBuilder(
        "bitrise-build-cache",
        "register-child-invocation",
        "--parent-id=$it",
        "--child-id=$invocationId",
        "--build-tool=gradle",
    )...
}
```

If `BITRISE_INVOCATION_ID` is unset the plugin does nothing — there is no parent to relate to. The Gradle invocation is still reported via the gradle-analytics service through its own channel; only the relation is gated on the env var.

### Generic external callers

Any tool that is invoked under `BITRISE_INVOCATION_ID` and wants to show up as a child in the analytics UI can shell out:

```bash
bitrise-build-cache register-child-invocation \
  --parent-id="$BITRISE_INVOCATION_ID" \
  --child-id="$MY_INVOCATION_ID" \
  --build-tool=mytool
```

Prerequisite: `~/.bitrise/analytics/multiplatform/config.json` must exist (auth source). It is created by any of the `activate` subcommands (`activate react-native`, `activate c++`, `activate xcode`).

---

## Programmatic API (Go)

The same backend calls are exposed as a Go API at `pkg/common`:

```go
type InvocationRegistry struct { ... }

func NewInvocationRegistry(params InvocationRegistryParams) (*InvocationRegistry, error)
func (inv *InvocationRegistry) RegisterMultiplatformInvocation(ctx context.Context, params RegisterInvocationParams) error
func (inv *InvocationRegistry) RegisterRelation(ctx context.Context, params RegisterRelationParams) error
```

`NewInvocationRegistry` reads the multiplatform analytics config from disk (auth + debug-logging flag). Pass `Envs` to override the metadata source.

This is the path used by every CLI-internal caller. The two cobra commands are thin wrappers around it; external callers can either invoke the CLI or import `pkg/common` directly.
