# Xcode compile caching: current state + planned changes

## What ships in this PR

`bitrise-build-cache activate xcode` wires Xcode compile caching through our proxy:

1. **xcconfig override** at `~/.bitrise-xcelerate/xcode-app.xcconfig`, included per-project by `bitrise-build-cache xcode link` via a marker-fenced `baseConfigurationReference` block. Carries:
   - `COMPILATION_CACHE_ENABLE_CACHING = YES`
   - `COMPILATION_CACHE_ENABLE_PLUGIN = YES`
   - `COMPILATION_CACHE_PLUGIN_PATH = …/libToolchainCASPlugin.dylib` (Apple's stock plugin)
   - `COMPILATION_CACHE_REMOTE_SERVICE_PATH = <proxy socket>`
   - `OTHER_SWIFT_FLAGS = $(inherited) -cas-plugin-option remote-service-path=<proxy socket>`
   - `TOOLCHAINS = io.bitrise.cas.xctoolchain`
2. **Thin toolchain bundle** installed at `~/.bitrise-xcelerate/toolchain/io.bitrise.cas.xctoolchain/`, surfaced to Xcode via a symlink under `~/Library/Developer/Toolchains/`. `ToolchainInfo.plist` carries `OverrideBuildSettings` with the same CAS keys above. Shadows the active Xcode's default toolchain bins/libs via symlinks — no code signing needed.
3. **Enrichment** at `internal/xcelerate/enrichment/`. The proxy's manifest watcher sees each completed Xcode build, parses the sibling `.xcactivitylog` for Apple's `compilation-cache-hit-rate` line, mints an invocation ID, and PUTs the invocation (with `HitRate` populated) to `xcode-analytics-service`.
4. **`doctor` surfacing** of the most recent `.xcactivitylog` under `~/Library/Developer/Xcode/DerivedData/*/Logs/Build/` so a developer can confirm cache behavior locally without the backend dashboard.

### Target coverage matrix (as of Xcode 27.0 / build `27A266a`)

| Build mode | App target | SPM package targets |
|---|---|---|
| CLI `xcodebuild` (toolchain + xcconfig) | ✅ | ✅ |
| Xcode.app IDE (toolchain + xcconfig) | ✅ | ❌ (local cache only) |

CLI mode works end-to-end for SPM because SwiftBuild's CLI path respects the toolchain's `OverrideBuildSettings` (including `OTHER_SWIFT_FLAGS`) on every target in the build graph. IDE mode works for app targets via the xcconfig include but drops the toolchain's `OTHER_SWIFT_FLAGS` from compile commands — SPM targets never see the remote CAS plugin option.

Validated end-to-end on 2026-10-05 against a CasProbe fixture with `swift-collections` dependency:
- CLI build via custom toolchain: `130 hits / 130 cacheable tasks (100%)`, invocation PUT landed in BE (`c47d69ca-cf98-45cb-8087-7432738c7f33`).
- Xcode.app IDE build on the same project: app-target hits counted correctly but SPM package compiles stayed local-only.

## Planned changes

### v2 — IDE SPM coverage via Xcode 27.1+ (no CLI change)

Apple's `swiftlang/swift-build` commit [`14cb1b0989b8`](https://github.com/swiftlang/swift-build/commit/14cb1b0989b8) (2026-07-24, "[CAS] Serialize remote service path into .cas-config and unify plugin options") teaches SwiftBuild's `CompilationCachingConfigFileTaskProducer` to write `remote-service-path` into every target's `.cas-config` `PluginOptions`. Xcode 27.0 ships SwiftBuild from before this commit — our `.cas-config` output only has `CASPath` + `PluginPath`, missing `PluginOptions`. Once Xcode 27.1 (or later) picks up the newer SwiftBuild, the toolchain's `COMPILATION_CACHE_REMOTE_SERVICE_PATH` setting automatically flows to `.cas-config` for IDE builds too, including SPM package targets. **No code change needed on our side** — just docs flip saying "IDE SPM coverage: ✅".

### v3 — proxy-side peer-PID session correlation

Replace the xcactivitylog-parse enrichment path with proxy-observed ground-truth counts. On every `Accept()` of the proxy's Unix socket, read peer PID via `getsockopt(SOL_LOCAL, LOCAL_PEERPID)` + walk ancestry (`ps -o ppid=`). SwiftBuild spawns one `SWBBuildService` per build (confirmed in spike for CLI mode: `peer_pid=71936 ancestry=71936(SWBBuildService)->71934(xcodebuild)->…`), and all CAS RPCs for that build arrive over the connections from that process. Mint one session per connection, aggregate observed hits/misses/bytes, emit the enriched invocation PUT on inactivity timeout or connection close.

Benefits over xcactivitylog parse:
- Proxy counts are ground truth — no self-report tamper surface.
- Correlation signal is kernel-level — doesn't rely on Apple's plugin cooperation.
- IDE vs CLI classification baked into the ancestry walk (parent is `Xcode` vs `xcodebuild`).

Not required to ship this PR, but unblocks a strict analytics signal path. Can land independently once Xcode 27.1 makes IDE builds reach the proxy for SPM too.

## Unpursued paths (recorded briefly to prevent re-exploration)

- **`-cas-plugin-option session-id=<uuid>` for in-band correlation.** Validated empirically that Apple's `libToolchainCASPlugin.dylib` consumes the option internally and never forwards it in gRPC metadata or request bodies. Not usable as a correlation signal unless Apple changes plugin behavior.
- **Signed `.pkg` toolchain distribution.** Not required — local installation under `~/Library/Developer/Toolchains/` works unsigned on current Xcode. Signing becomes necessary only if we later distribute via `.pkg` for machines where the user can't drop a bundle directly.
- **Custom CAS plugin fork** (prior analysis `docs/xcode-cas-plugin-fork-analysis-2026-10-01.md`, not on this branch). The toolchain + `OverrideBuildSettings` approach reaches SPM without a fork. Fork remains an option if Apple ever closes the toolchain path.
