# Bitrise Build Cache CLI

Bitrise Build Cache CLI - to enable/configure Gradle or Bazel build cache on the machine where you run this CLI.

> [!IMPORTANT]
> **`install/installer.sh` and the assets attached to every CLI GitHub release are on the critical path of every Bitrise build — not just builds that opt into the build cache.**
>
> The Bitrise default workflow runs the gradle-mirrors activation step (and other CLI-driven steps) unconditionally, and each of those installs the CLI by piping `install/installer.sh` to `sh` and fetching the platform tarball + checksum from the latest non-prerelease GitHub release. If any of those (the installer script, the binaries, the checksum file) is broken or missing, the CLI install fails, the mirror activation soft-fails, and Maven Central requests stop going through the Bitrise proxy on the entire fleet — exactly the failure mode behind the [2026-04-28 Maven Central rate-limit incident](https://bitrise.atlassian.net/wiki/spaces/INCIDENT/pages/4980998155/2026-04-28+-+Postmortem+for+incident-2026-04-28-mavencentral-too-many-requests-5238).
>
> When changing the installer script, the goreleaser config, the release flow, or anything that affects the GitHub release's asset list:
> - Smoke-test end-to-end on a real Bitrise build before merging.
> - Never publish a CLI GitHub release as anything other than `--prerelease` until the binaries have been verified attached.
> - Treat any failure of the `release` workflow as a critical, drop-everything-and-fix incident.
>
> **GAR fallback mirror.** Every CLI release also mirrors the platform tarballs, the checksums file, **and `install/installer.sh` itself** to a public GAR generic repository (`build-cache-cli-releases` in `ip-build-cache-prod`, region `us-central1`). The mirror is used by `installer.sh` whenever the GitHub primary path fails — both for downloading the binary (already in place) and for resolving the latest tag when `github.com` is unreachable (via the `installer.sh:latest-pointer:VERSION` discovery file). The Bitrise preboot init scripts (`bitrise-io/build-prebooting-deployments`) also fall back to GAR when `raw.githubusercontent.com` is degraded. Binaries and the pinned `installer.sh:<tag>:installer.sh` are **immutable** (describe-or-upload, see #327 postmortem); the documented carve-out is `installer.sh:latest-pointer:*`, which is mutable by design and only consulted on the already-degraded fallback path. A separate `verify-release` workflow (chained after `release` via the `release-and-verify` pipeline) runs `scripts/verify_release.sh` to assert both the GH and GAR-only install paths work end-to-end; failures here post to Slack and can be retried independently without re-cutting the tag.


## Install

**For the full local-dev install guide — Homebrew, `installer.sh`, GAR fallback, credentials, verify, troubleshooting — see [`docs/install.md`](docs/install.md).**

Minimum copy-paste:

```shell
brew install bitrise-io/bitrise-build-cache/bitrise-build-cache
# — or —
curl --retry 5 -sSfL 'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' | sh -s -- -b ~/.local/bin
```

Authentication is via two env vars (PAT + workspace ID) — see the [post-install section](docs/install.md#post-install) for how to obtain them and where to set them.

> The CLI configures the environment it's running in. If you're running commands in Docker containers, run the CLI inside the same container as Gradle/Bazel/Xcode/ccache.

### Xcode.app (GUI builds)

Pressing ▶ in Xcode.app bypasses the `xcodebuild` wrapper. Use `bitrise-build-cache xcode-app enable` to install an `XCODE_XCCONFIG_FILE` override so the GUI build pipeline also picks up the cache. See [`docs/xcode-app.md`](docs/xcode-app.md) for the full flow + a repo-controlled helper script pattern for team-wide rollout. macOS only.


## What does the CLI do on a high level?

It creates the necessary config to enable Build Cache and Command Exec/Invocation Analytics. It does this via adding the config in the `$HOME` directory.

In case of Gradle it's done via creating or modifying the following two files: `$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts` and `$HOME/.gradle/gradle.properties` (adding `org.gradle.caching=true` to `gradle.properties`).

In case of Bazel it's done via creating or modifying `$HOME/.bazelrc`.


## High level description of the process

When `activate gradle` or `activate bazel` is called:

1. CLI checks whether all the available inputs are available. Inputs (auth token, workspace ID, ...) are read from environment variables or via flags specified for the command.
2. Then it checks whether the configuration file(s) already exist in the `$HOME` directory.
4. Then it generates the build cache configuration content (merging with the current content of the configuration file(s) if the file(s) already exist).
5. And then it writes the configuration content into the config file(s).


## Technical notes

### Gradle

- `$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts` is overwritten when you run `activate gradle`.
  Any modification you do in that file will be overwritten.
- `$HOME/.gradle/gradle.properties` is modified in the following way: when you run `activate gradle`
  the CLI will check whether a `# [start] generated-by-bitrise-build-cache / # [end] generated-by-bitrise-build-cache`
  block is already in the file. If there is, then only the block's content will be modified.
  If there's no marked block in the properties file yet then the CLI will append it to the file
  with the necessary content in the block (`org.gradle.caching=true`).
- The CLI will also try to download the Bitrise gradle plugins from the Build Cache to avoid having to rely on maven central. 

### Bazel

- `$HOME/.bazelrc` is modified in the following way: when you run `activate bazel`
  the CLI will check whether a `# [start] generated-by-bitrise-build-cache / # [end] generated-by-bitrise-build-cache`
  block is already in the file. If there is, then only the block's content will be modified.
  If there's no marked block in the bazelrc file yet then the CLI will append it to the file
  with the necessary content in the block.

## Package architecture

The codebase follows a three-layer architecture with strict dependency direction:

```
┌─────────────────────────────────────────────────────┐
│  cmd/                                               │
│  Thin cobra wrappers — map flags to params,         │
│  call pkg/ structs. No business logic.              │
│                                                     │
│  cmd/ccache/     cmd/reactnative/   cmd/gradle/ ... │
└──────────────┬──────────────────────────────────────┘
               │ imports
               ▼
┌─────────────────────────────────────────────────────┐
│  pkg/                                               │
│  Public API for external Go packages (e.g. steps).  │
│  Exported structs with public methods.              │
│  No cobra dependency.                               │
│                                                     │
│  pkg/ccache/          pkg/reactnative/              │
│  ├── StorageHelper    ├── Activator                 │
│  ├── Activator        ├── Runner                    │
│  └── InvocationReg.   └── (postRunDeps internal)    │
└──────────────┬──────────────────────────────────────┘
               │ imports
               ▼
┌─────────────────────────────────────────────────────┐
│  internal/                                          │
│  Core business logic, config, protocols, analytics. │
│  Not importable outside this module.                │
│                                                     │
│  internal/config/     internal/ccache/              │
│  internal/xcelerate/  internal/build_cache/kv/      │
└─────────────────────────────────────────────────────┘
```

**Dependency rules:**
- `cmd/` imports `pkg/` and `internal/` (for flag types and wiring)
- `pkg/` imports `internal/` only — never `cmd/`
- `internal/` never imports `cmd/` or `pkg/`

**Using `pkg/` from external Go code (e.g. Bitrise steps):**

Instead of shelling out to the CLI binary, Go packages can import the `pkg/` structs directly:

```go
import ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/pkg/ccache"

// Start the ccache storage helper
helper, err := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
    InvocationID: myID,
    DebugLogging: true,
})
if err != nil { ... }
err = helper.Start(ctx)
```

```go
import rnpkg "github.com/bitrise-io/bitrise-build-cache-cli/pkg/reactnative"

// Activate React Native build cache
a := &rnpkg.Activator{
    Params: rnpkg.ActivatorParams{Gradle: true, Xcode: true, Cpp: true},
}
err := a.Activate(ctx)
```

## Release process

Refer to the [confluence page](https://bitrise.atlassian.net/wiki/spaces/RD/pages/3620110397/Build+cache+plugin+CLI+release+flow)
