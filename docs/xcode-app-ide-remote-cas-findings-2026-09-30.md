# Xcode.app IDE remote CAS — Xcode 27 retest + Tuist claim check 2026-09-30

## TL;DR (updated — Apple's plugin actually works!)

**Apple's stock `libToolchainCASPlugin.dylib` connects to Bitrise's xcelerate-proxy over the unix socket and remote CAS engages end-to-end.** 130 hits / 130 cacheable tasks (100%) reported by Xcode, 270 remote loads confirmed in the proxy log. Prior 2026-07-21 verdict ("IDE can't reach remote") was wrong. No wrapper dylib is needed — Apple's plugin and our proxy speak the same LLVM CAS gRPC protocol (our proxy already implements `CASDBService`, `KeyValueDB`, `Session` from `swiftlang/swift-build`'s protos).

To engage remote CAS from `xcodebuild` (and, by extension, Xcode.app IDE):

```
CLANG_ENABLE_COMPILE_CACHE = YES
CLANG_ENABLE_MODULES = YES
COMPILATION_CACHE_ENABLE_CACHING = YES
COMPILATION_CACHE_ENABLE_PLUGIN = YES
COMPILATION_CACHE_PLUGIN_PATH = /Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib
COMPILATION_CACHE_REMOTE_SERVICE_PATH = /var/folders/…/T/xcelerate-proxy.sock
SWIFT_ENABLE_COMPILE_CACHE = YES
OTHER_SWIFT_FLAGS = $(inherited) -cas-plugin-option remote-service-path=/var/folders/…/T/xcelerate-proxy.sock
```

Two gotchas that ruined all prior tests:

1. **Do NOT set `COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES`.** SwiftBuild's `CompilationCachingConfigFileTaskProducer` has a FIXME that bails when both `REMOTE_SERVICE_PATH` and `SUPPORTED_LANGUAGES` are set (`return nil`, no `.cas-config` written).
2. **Settings must reach the *target* scope, not just workspace/project.** `XCODE_XCCONFIG_FILE` doesn't propagate into SPM package targets — those need the settings baked into their own generated xcconfigs (which is what `tuist generate` does for its projects). For Xcode-native projects the env-var override works.

### Original TL;DR (now obsolete — kept for context)

- **Prior investigation (2026-07-21) was partially wrong.** The "xcspec has only 3 keys, everything else silently dropped" verdict is not the full story.
- `COMPILATION_CACHE_PLUGIN_PATH` and `COMPILATION_CACHE_REMOTE_SERVICE_PATH` are declared as first-class `BuiltinMacros` in SWBCore (Xcode's build system). They **do** get read at task-construction time — they are just not in `CoreBuildSystem.xcspec` (which is a red herring).
- With the right xcconfig, we can get:
  - swiftc argv gain `-cas-plugin-path <path>` (SWBCore wires this)
  - `.cas-config` gains `"PluginPath": "…"` (task producer writes it)
  - swiftc argv gain `-cas-plugin-option remote-service-path=<sock>` (via `OTHER_SWIFT_FLAGS`)
- **Apple's stock `libToolchainCASPlugin.dylib` ships with `RemoteService` + gRPC + `remote-service-path` option support baked in** (visible in `strings`).
- ~~**But Apple's plugin never opens our socket.** The plugin speaks Apple's private gRPC protocol; our proxy speaks the Bitrise Accelerate protocol. Wire is up, protocol mismatch.~~ **WRONG — see "Retest with proxy debug on" below.** Apple's plugin connects fine; both sides speak the same LLVM CAS gRPC (`swiftlang/swift-build`'s protos). Prior "no connection" was a false negative from our proxy log verbosity.
- **Tuist's claim is real** — they engage remote CAS from Xcode.app IDE. Their mechanism is a **custom CAS plugin** (`libtuist_cas_plugin.dylib`, Rust cdylib wrapping Apple's plugin with Tuist-remote read/write-through) that they ship + wire via `COMPILATION_CACHE_PLUGIN_PATH`. See "Tuist's actual mechanism" below.

## Test host

macOS 26.6.2 (25G83) · Xcode 27.0 (27A266a) · CLI `devel` (main).
Projects: `/Users/balazs.hajagos/dev/spike-icecubes` (IceCubesApp, SPM-heavy) and `/tmp/cas-probe` (minimal non-SPM iOS app scaffolded with `xcodegen`).

## What we actually proved on Xcode 27

### xcspec is a red herring

```
$ grep -oE 'COMPILATION_CACHE[A-Z_]*' \
    /Applications/Xcode.app/…/CoreBuildSystem.xcspec | sort -u
COMPILATION_CACHE_ENABLE_CACHING
COMPILATION_CACHE_ENABLE_CACHING_DEFAULT
COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS
```

Three keys — same as Xcode 26.6. But SWBCore also hard-codes the rest:

```
$ strings /Applications/Xcode.app/…/SWBCore.framework/…/SWBCore | grep -E \
    "COMPILATION_CACHE_PLUGIN_PATH|COMPILATION_CACHE_REMOTE_SERVICE_PATH|cas-plugin-path"
-cas-plugin-path
COMPILATION_CACHE_PLUGIN_PATH
COMPILATION_CACHE_REMOTE_SERVICE_PATH
COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES
```

Upstream source (`swiftlang/swift-build`) confirms — `Sources/SWBCore/Settings/BuiltinMacros.swift`:

```swift
public static let COMPILATION_CACHE_ENABLE_PLUGIN = BuiltinMacros.declareBooleanMacro("COMPILATION_CACHE_ENABLE_PLUGIN")
public static let COMPILATION_CACHE_PLUGIN_PATH = BuiltinMacros.declareStringMacro("COMPILATION_CACHE_PLUGIN_PATH")
public static let COMPILATION_CACHE_REMOTE_SERVICE_PATH = BuiltinMacros.declareStringMacro("COMPILATION_CACHE_REMOTE_SERVICE_PATH")
public static let COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES = BuiltinMacros.declareStringListMacro("COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES")
```

These are all first-class build settings, not user-defined.

### CASOptions creation (`Sources/SWBCore/Settings/CASOptions.swift`)

```swift
if scope.evaluate(BuiltinMacros.COMPILATION_CACHE_ENABLE_PLUGIN) {
    casPath = scope.evaluate(BuiltinMacros.COMPILATION_CACHE_CAS_PATH).join("plugin")
    pluginPath = Path(scope.evaluate(BuiltinMacros.COMPILATION_CACHE_PLUGIN_PATH))
    let remoteServicePathSetting = Path(scope.evaluate(BuiltinMacros.COMPILATION_CACHE_REMOTE_SERVICE_PATH))
    if !remoteServicePathSetting.isEmpty && isLanguageSupportedForRemoteCaching() {
        remoteServicePath = remoteServicePathSetting
    }
} else {
    casPath = scope.evaluate(BuiltinMacros.COMPILATION_CACHE_CAS_PATH).join("builtin")
    pluginPath = nil
    remoteServicePath = nil
    if !scope.evaluate(BuiltinMacros.COMPILATION_CACHE_REMOTE_SERVICE_PATH).isEmpty {
        delegate?.warning("… is set but COMPILATION_CACHE_ENABLE_PLUGIN is not enabled; the remote CAS service will not be used")
    }
}
```

- `CASPath` ends in `/plugin` vs `/builtin` tells you which branch ran.
- Our prior investigation always saw `/builtin` because it never set `COMPILATION_CACHE_ENABLE_PLUGIN=YES` (or the setting didn't reach the target scope — see "SPM scope" below).

### `.cas-config` producer (`CompilationCachingConfigFileTaskProducer.swift`)

```swift
// FIXME: we need consistent CAS configuration across all languages.
if !scope.evaluate(BuiltinMacros.COMPILATION_CACHE_REMOTE_SERVICE_PATH).isEmpty
    && !scope.evaluate(BuiltinMacros.COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES).isEmpty {
    return nil
}
```

**When both `REMOTE_SERVICE_PATH` and `SUPPORTED_LANGUAGES` are set, the producer bails and writes no `.cas-config`.** Our prior xcconfig set both, so no `.cas-config` was ever written by the producer — but Xcode still writes a *fallback* `.cas-config` with just `CASPath` somewhere earlier in the pipeline, which is what we kept seeing.

Drop `SUPPORTED_LANGUAGES` → producer runs → `.cas-config` gains `PluginPath`.

### Confirmed on `/tmp/cas-probe` (minimal non-SPM iOS app)

xcconfig:

```
CLANG_ENABLE_COMPILE_CACHE = YES
CLANG_ENABLE_MODULES = YES
COMPILATION_CACHE_ENABLE_CACHING = YES
COMPILATION_CACHE_ENABLE_PLUGIN = YES
COMPILATION_CACHE_PLUGIN_PATH = /Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib
COMPILATION_CACHE_REMOTE_SERVICE_PATH = /var/folders/xs/…/T/xcelerate-proxy.sock
SWIFT_ENABLE_COMPILE_CACHE = YES
OTHER_SWIFT_FLAGS = $(inherited) -cas-plugin-option remote-service-path=/var/folders/xs/…/T/xcelerate-proxy.sock
```

Build via `XCODE_XCCONFIG_FILE=… /usr/bin/xcodebuild build`. Results:

- swiftc argv:

    ```
    -cas-path       /Users/…/CompilationCache.noindex/plugin       ← "/plugin", ENABLE_PLUGIN wired
    -cas-plugin-path /Applications/Xcode.app/…/libToolchainCASPlugin.dylib
    -cas-plugin-option remote-service-path=/var/folders/xs/…/T/xcelerate-proxy.sock
    ```

- `.cas-config`:

    ```json
    {"CASPath":"/Users/…/CompilationCache.noindex/plugin",
     "PluginPath":"/Applications/Xcode.app/…/libToolchainCASPlugin.dylib"}
    ```

    (PluginPath present. Not just CASPath.)

- Proxy log during the build: **still 0 upload/get lines**.

### Apple's plugin has the machinery — but doesn't use our proxy

```
$ strings /Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib | \
    grep -iE "RemoteService|remote-service-path|remote_id|token-hash|endpoint" | sort -u
RemoteService
_TtC18ToolchainCASPluginP…RemoteService
_remoteService
endpoint
remote-service-path
remote_id
token-hash
```

The plugin **does** understand `remote-service-path` (+ related `remote_id`, `token-hash`, `endpoint` options). It also has full gRPC symbols. So the plugin is capable of remote — it just doesn't open our socket.

Best explanation: the plugin speaks **Apple's private LLVM-CAS gRPC protocol**, which is not the Bitrise Accelerate gRPC protocol our proxy implements. The plumbing engages; the two sides don't understand each other. No public documentation of Apple's expected wire protocol has surfaced in searches.

### Bonus lesson: SPM package targets don't inherit `XCODE_XCCONFIG_FILE`

On IceCubesApp (which is 30+ SPM packages), settings resolved at the workspace level but SPM package targets got `CASPath = …/builtin` (i.e. `ENABLE_PLUGIN` evaluated FALSE inside their scope). That's why our first Xcode-27 pass saw `{"CASPath":…}` everywhere and mistook it for a silent drop.

Confirmed by rerunning on `/tmp/cas-probe` (non-SPM): `.cas-config` gains `PluginPath`, swiftc argv gains `-cas-plugin-path`. On the SPM case, both are absent.

`xcodebuild` CLI also doesn't inherit `launchctl setenv` — you must `export XCODE_XCCONFIG_FILE=…` in the shell. `Xcode.app` (GUI) does inherit it at launch.

## Retest with proxy debug on — Apple's plugin engages remote CAS

Prior test proxy logs (default verbosity) showed no upload/get lines and we called that a protocol mismatch. Rerun with `bitrise-build-cache -d xcelerate start-proxy` and every request from Apple's plugin is visible:

```
[13:52:04] Load with key xcelerate-cas-662e…7ce0 took 2.33s and was a hit: true
[13:52:04] Downloaded xcelerate-cas-662e…7ce0 hash matches expected: 1f29…f56b
… (270 loads over the build, all hit=true)
```

Xcode's own report at the end of the build:

```
CompilationCacheMetrics
note: 130 hits / 130 cacheable tasks (100%)
```

Reproducer (`/tmp/cas-probe`, minimal non-SPM iOS app):

1. `bitrise-build-cache activate xcode`
2. `bitrise-build-cache -d xcelerate start-proxy &`
3. xcconfig above → `XCODE_XCCONFIG_FILE=…` + `/usr/bin/xcodebuild … build`
4. Proxy debug log shows `Load with key xcelerate-cas-…` lines.
5. Xcode's `CompilationCacheMetrics` line at the tail of the build reports the hit rate.

The build itself failed on Info.plist (xcodegen omitted it). Every SwiftCompile that actually ran was 100% remote-cached.

### Why our proxy is compatible

Our `internal/xcelerate/proxy/proxy.go` `Proxy` type implements:

```go
_ llvmcas.CASDBServiceServer = (*Proxy)(nil)
_ llvmkv.KeyValueDBServer    = (*Proxy)(nil)
_ session.SessionServer      = (*Proxy)(nil)
```

The protos under `proto/llvm/{cas,kv,session}` come straight from `llvm/llvm-project` (`compilation_caching_cas.proto`, `compilation_caching_kv.proto`, `session.proto`, `java_package = "com.apple.dt.compilation_cache_service"`). Apple's `libToolchainCASPlugin.dylib` speaks the exact same protocol; the "private gRPC protocol" from the previous section was a bad guess.

## Tuist's actual mechanism (from `tuist/tuist` source)

Tuist's remote Xcode compilation cache is real and works from Xcode.app IDE. From `cas-plugin/AGENTS.md`:

> Xcode passes `-cas-plugin-option <name>=<value>` flags to the plugin via `llcas_cas_options_set_option`, sourced from build settings (`tuist generate` bakes them into `OTHER_SWIFT_FLAGS`). Unlike the environment, these reach **every** compiler frontend — including an Xcode ⌘B build that carries no CLI environment — which is how the plugin learns its instance without the CLI.

And `AGENTS.md` at the repo root:

> `cas-plugin/` — Xcode compilation-cache CAS plugin (**Rust cdylib wrapping Apple's libToolchainCASPlugin with Tuist-remote read/write-through**) — see `cas-plugin/AGENTS.md`

Their setup instructions (from `server/priv/docs/en/guides/features/cache/xcode-cache.md`):

```
COMPILATION_CACHE_ENABLE_CACHING = YES
COMPILATION_CACHE_ENABLE_PLUGIN = YES
COMPILATION_CACHE_PLUGIN_PATH = $HOME/.local/state/tuist/libtuist_cas_plugin.dylib
COMPILATION_CACHE_REMOTE_SERVICE_PATH = $HOME/.local/state/tuist/cas-proxy.sock
COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS = YES
OTHER_SWIFT_FLAGS = $(inherited) -cas-plugin-option tuist-instance=your-org/your-project
```

Key points:

1. **`COMPILATION_CACHE_PLUGIN_PATH` points at their own `libtuist_cas_plugin.dylib`**, not Apple's. Their Rust dylib re-exports the LLVM CAS plugin C ABI, wraps Apple's plugin for local, and adds Tuist-remote read/write-through.
2. **`tuist-instance` is Tuist's own plugin option**, not Apple's — their plugin reads it via `llcas_cas_options_set_option` and uses it to route to the correct project on the backend.
3. **`tuist setup cache` installs a `LaunchAgent`** that runs `tuist-cas-proxy` on a Unix socket. Their proxy implements whatever protocol *their plugin* speaks (they own both sides).
4. **`tuist generate` bakes the settings into every generated `.xcconfig`**. Bypasses the SPM-scope problem entirely — for Tuist-generated projects, every target sees the settings.
5. Manual setup works too (they document copy-pasting the block into project build settings or an `xcconfig`), but you're on the hook for baking it into every target.

The `tuist/tuist/cas-plugin/AGENTS.md` file (fetched via `gh api`) makes it explicit: **they wrap Apple's plugin, they don't try to reuse it.** They only inherit local-cache behavior; the remote leg is theirs end-to-end.

## What this means for us

**Xcode.app IDE remote CAS with Bitrise's proxy already works.** No custom plugin dylib required. What we need to ship:

1. **New activation subcommand / xcconfig writer** that emits the six-line block above into either:
   - The user's project xcconfig (opt-in `#include?` chain, matches the removed `xcode-app link` model), or
   - A launchctl-installed `XCODE_XCCONFIG_FILE` (matches the removed `xcode-app enable` model — reintroduce it, this time it does the whole job).
2. **Explicit "do not set SUPPORTED_LANGUAGES" guard** in the activation code + the docs; the FIXME in `CompilationCachingConfigFileTaskProducer` bites hard.
3. **SPM caveat in the docs.** For workspaces heavy on SPM packages the env-var trick only covers the app target scope; SPM package targets don't inherit. Two options for those:
   - Ask users to add the settings to each package via `swiftSettings` in `Package.swift` (Package targets don't expose OTHER_SWIFT_FLAGS easily but do expose `-cas-plugin-option` via `.unsafeFlags`).
   - Ship a `bitrise-build-cache xcode-app inject-into-workspace` command that walks each `.xcodeproj` in the workspace and adds the include to every base xcconfig at build-settings level.
4. **Diagnostic-remarks toggle.** Xcode's `CompilationCacheMetrics note: N hits / M cacheable tasks (X%)` is a great signal to expose in `doctor`. The plugin already emits per-key `local cache miss for key: <base64>` when `COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS=YES` — useful for local debugging.
5. **Keep the wrapper `xcodebuild` CLI path.** Still needed for hit-rate telemetry / invocation IDs / enrichment. IDE users who don't want the wrapper get remote CAS from the xcconfig alone; wrapper users get remote CAS + analytics.

The wrapper plugin ideas (Tuist-style Rust cdylib, Apple-plugin fork, fswatch, custom toolchain, Apple Feedback) are no longer necessary. They remain valid for feature parity with things Tuist has that Apple's plugin doesn't (server-side chunking, per-project remote_id-scoped stores, upload-only-from-CI knob), but none of those unblock the core "IDE builds hit remote" story.

Only case where we'd still want our own plugin: if we later need behavior Apple's plugin can't do (e.g. rebase artifact keys, transform blobs before store, expose custom `-cas-plugin-option` knobs). File-under "future work, not blocking."

## SPM package target scope — CLI works, IDE doesn't

Follow-up 2026-09-30 (same session): can we reach SPM package targets from `xcodebuild` CLI? From Xcode.app IDE? Result:

**CLI, yes — no Package.swift patching needed:**

```
xcodebuild ... \
  COMPILATION_CACHE_ENABLE_PLUGIN=YES \
  COMPILATION_CACHE_PLUGIN_PATH=/Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib \
  COMPILATION_CACHE_REMOTE_SERVICE_PATH=<sock> \
  OTHER_SWIFT_FLAGS='$(inherited) -cas-plugin-option remote-service-path=<sock>' \
  build
```

`xcodebuild`'s command-line `KEY=VAL` overrides propagate to every target scope including SPM package targets. Verified on IceCubesApp: LRUCache SPM package's `.cas-config` gained `"PluginPath":"…"`, our proxy log showed 2810 remote loads (76% remote-cache hit rate — 2810 hit + 902 miss). Build failed for an unrelated Swift-source error (`'Document' is ambiguous`) so Xcode's `CompilationCacheMetrics` reported `0 hits / 736`, but the remote plumbing was live throughout. Our existing xcodebuild wrapper can add these overrides automatically.

**IDE, no — several angles all fail:**

- **`.unsafeFlags` in Package.swift** — resolve-time error: `the target 'X' in product 'Y' contains unsafe build flags`. Not usable, even under `.when(configuration: .debug)`. Products that contain unsafe flags can't be consumed downstream.
- **`XCODE_XCCONFIG_FILE` (env-inherited by Xcode.app at launch)** — settings resolve at the root project scope but SPM package target scope does not inherit them (verified: LRUCache `.cas-config` stays `{"CASPath":".../builtin"}`).
- **Direct-launch Xcode.app with env vars** (`env COMPILATION_CACHE_ENABLE_PLUGIN=YES … /Applications/Xcode.app/Contents/MacOS/Xcode …`) — verified: the env reaches the Xcode.app process, but SwiftBuild does not map these env vars to build settings for SPM package scope. `.cas-config` for LRUCache still builtin-only.
- **Scheme pre-action patching Package.swift** — same `.unsafeFlags` wall.
- **`-Xswiftc` on xcodebuild** — not a xcodebuild flag (SPM-only).

**Realistic paths for IDE+SPM coverage** (none simple, none prototyped):

1. **Tuist-style project generation** — convert SPM packages to real Xcode targets so their settings live in `project.pbxproj` (where xcconfig chains apply). Huge scope; user's project shape changes; probably a non-starter for existing customers.
2. **Ship a swiftc shim on PATH ahead of Xcode's toolchain** — Xcode uses absolute paths (`XcodeDefault.xctoolchain/usr/bin/swiftc`), so PATH shims don't win. Would need a custom `~/Library/Developer/Toolchains/` toolchain that the user selects — nuclear, brittle across Xcode updates.
3. **Live with partial IDE coverage.** Document: IDE remote CAS covers the root project and any hand-written non-SPM targets; SPM package targets remain local-only in IDE. CLI users get full coverage automatically.

Recommended shipping order: (1) auto-inject the four settings in the xcodebuild wrapper for full CLI coverage, (2) reintroduce `xcode-app` command surface with an `XCODE_XCCONFIG_FILE`-based activator for IDE-mode partial coverage, (3) document the IDE+SPM gap prominently.

## Corrections against the 2026-07-21 doc

- "xcspec allowlist has only 3 keys → silent drop" — misleading. `PLUGIN_PATH` and `REMOTE_SERVICE_PATH` are wired via SWBCore's `BuiltinMacros`, not xcspec.
- "The plugin's own JSON schema supports `RemoteService` … but Xcode's build system has no build setting that maps to it" — wrong on the second half. It does; you just need `ENABLE_PLUGIN=YES` + `PLUGIN_PATH` + `REMOTE_SERVICE_PATH` reaching the *target* scope.
- The doc's option #4 ("Fork libToolchainCASPlugin.dylib — blocked, `-cas-plugin-path` locked to Xcode.app internal path") is wrong. `COMPILATION_CACHE_PLUGIN_PATH` is a first-class build setting; you can point `-cas-plugin-path` anywhere. That's how Tuist ships their fork.

The 2026-07-21 headline (IDE doesn't reach *our* remote) is still true. The reason is different — protocol mismatch, not "Xcode drops the setting."

## Reproducer (updated)

```sh
# Cold caches
osascript -e 'tell application "Xcode" to quit'
DD="$HOME/Library/Developer/Xcode/DerivedData"
rm -rf "$DD"/CasProbe-* "$DD/CompilationCache.noindex" \
       "$DD/ModuleCache.noindex" "$DD/SDKStatCaches.noindex"

# Minimal non-SPM iOS project
mkdir -p /tmp/cas-probe && cd /tmp/cas-probe
cat > project.yml <<EOF
name: CasProbe
targets:
  CasProbe:
    type: application
    platform: iOS
    deploymentTarget: "17.0"
    sources: [Source]
EOF
mkdir -p Source
cat > Source/App.swift <<EOF
import SwiftUI
@main struct CasProbeApp: App { var body: some Scene { WindowGroup { Text("hi") } } }
EOF
xcodegen generate

# xcconfig — no SUPPORTED_LANGUAGES (defeats the FIXME bail)
cat > /tmp/cas-probe/probe.xcconfig <<EOF
CLANG_ENABLE_COMPILE_CACHE = YES
CLANG_ENABLE_MODULES = YES
COMPILATION_CACHE_ENABLE_CACHING = YES
COMPILATION_CACHE_ENABLE_PLUGIN = YES
COMPILATION_CACHE_PLUGIN_PATH = /Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib
COMPILATION_CACHE_REMOTE_SERVICE_PATH = /var/folders/…/T/xcelerate-proxy.sock
SWIFT_ENABLE_COMPILE_CACHE = YES
OTHER_SWIFT_FLAGS = \$(inherited) -cas-plugin-option remote-service-path=/var/folders/…/T/xcelerate-proxy.sock
EOF

bitrise-build-cache activate xcode
nohup bitrise-build-cache xcelerate start-proxy > /tmp/proxy.log 2>&1 &

XCODE_XCCONFIG_FILE=/tmp/cas-probe/probe.xcconfig /usr/bin/xcodebuild \
    -project /tmp/cas-probe/CasProbe.xcodeproj -scheme CasProbe \
    -destination 'generic/platform=iOS Simulator,name=iPhone 17' \
    -configuration Debug CODE_SIGNING_ALLOWED=NO CODE_SIGN_IDENTITY= build

# Inspect
find "$DD"/CasProbe-* -name .cas-config -exec cat {} \;
# → {"CASPath":".../plugin","PluginPath":"/Applications/Xcode.app/…/libToolchainCASPlugin.dylib"}

grep -oE '\-cas[A-Za-z-]* [^ ]+|remote-service-path[= ][^ ]+' /path/to/build.log | sort -u
# → -cas-path, -cas-plugin-path, -cas-plugin-option remote-service-path=…

grep -c "Upload\|xcelerate-cas" /tmp/proxy.log
# → 0  (Apple's plugin doesn't speak our protocol)
```

Swap `COMPILATION_CACHE_PLUGIN_PATH` for a plugin that DOES speak our proxy's protocol → uploads would start. Which is exactly what Tuist did.

## Follow-up: `enable` is not enough (2026-10-01)

The initial plan assumed `launchctl setenv XCODE_XCCONFIG_FILE` would propagate into Xcode.app IDE builds on Xcode 27+. Manual verification showed it does not.

Setup: `xcode-app enable` on Xcode 27.0 / macOS 26.6.2. `launchctl getenv XCODE_XCCONFIG_FILE` reports the override path correctly. Xcode.app launched two ways — `open -a Xcode <project>` and `env XCODE_XCCONFIG_FILE=... /Applications/Xcode.app/Contents/MacOS/Xcode <project>` — behaved identically: swiftc argv had no `-cas-*` flags, no `.cas-config` was written, zero proxy traffic. `lsof` on `SWBBuildService` confirmed the xcconfig file was never opened.

Fix that works: set the xcconfig as each `XCBuildConfiguration`'s `baseConfigurationReference` in `project.pbxproj`. Result on `/tmp/cas-probe`: 65/65 cacheable tasks cached (100%), 71 proxy loads delta from baseline.

Shipped as `xcode-app link <path>` (and `unlink`). Appends a marker-fenced `#include?` to each configuration's existing base xcconfig, or creates a sibling `.bitrise-build-cache.xcconfig` when there is none.
