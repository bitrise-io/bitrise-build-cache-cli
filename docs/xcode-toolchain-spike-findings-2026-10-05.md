# Xcode custom-toolchain spike for IDE / SPM compile-cache reach — 2026-10-05

## Why

Current `xcode link` injects a sibling xcconfig block (`#include?
~/.bitrise-xcelerate/xcode-app.xcconfig`) that patches each
`XCBuildConfiguration` so Apple's stock LLVM CAS plugin loads and talks to our
proxy. It works for app targets but **does not reach SPM package targets**
(SwiftBuild compiles packages through its own graph that doesn't inherit the
app's xcconfig baseConfigurationReference). This spike explored whether a
**custom Xcode toolchain** could carry the compile-cache wiring universally,
reaching SPM targets without per-project xcconfig per package.

## What was validated

### 1. Custom toolchain is discoverable + usable without signing (local dev)

- Minimal bundle: `ToolchainInfo.plist` + `usr/bin` + `usr/lib` symlinks to
  Default toolchain.
- Required plist keys: `Identifier`, `DisplayName`, `CompatibilityVersion`
  (integer, 2 for Xcode 15+), `CompatibilityVersionDisplayString`.
- Without `CompatibilityVersion` → Xcode.app toolchain menu lists it but shows
  "The toolchain is not compatible with this version of Xcode." dialog on
  select.
- Install via symlink: `~/Library/Developer/Toolchains/<id> → /path/to/bundle`.
- Xcode.app selected the toolchain from `Xcode → Toolchains` menu with **no
  signing prompt and no additional sandboxing hoops** on Xcode 26.6 /
  macOS Tahoe 26.
- `xcodebuild TOOLCHAINS=<id> …` + `defaults write com.apple.dt.Xcode
  DVTDefaultToolchain <id>` also work.

### 2. `OverrideBuildSettings` on toolchain propagates to every target

`ToolchainInfo.plist`'s `OverrideBuildSettings` dict is honoured by SwiftBuild
and applied to **every target in the build graph including SPM package
targets**. Confirmed by:

- `xcodebuild -showBuildSettings TOOLCHAINS=<id> …` lists the overridden
  values with correct resolution.
- Build log contains `note: Using global toolchain override 'Bitrise CAS (…)'`
  entries for both app target (`CasProbe`) and SPM targets (`Collections`,
  `BitCollections`, `InternalCollectionsUtilities`, etc.).
- End-to-end: CLI `xcodebuild` on `CasProbe` + `swift-collections` dep →
  compile cache plugin loaded, remote CAS served requests, `.xcactivitylog`
  contained `130 hits / 130 cacheable tasks (100%)` after warm run.

### 3. Peer-PID per-build correlation on the proxy socket works

Added a `peerPIDListener` wrapper around the proxy's `net.Listener` that calls
`getsockopt(SOL_LOCAL, LOCAL_PEERPID)` on each `Accept()` and walks the `ps`
ancestry. Confirmed for CLI builds:

```
peer_pid=71936 ancestry=71936(SWBBuildService)->71934(xcodebuild)->71846(zsh)->…
```

- **One build via `xcodebuild` CLI = exactly one new peer connection** from a
  freshly-spawned `SWBBuildService` process.
- `SWBBuildService` is SwiftBuild's compile orchestrator; its parent (`ps -o
  ppid=`) is `xcodebuild` for CLI builds — would be `Xcode` for IDE builds.
- Ancestry walk is a reliable source classifier (IDE vs CLI vs CI-step).
- Confirms the mechanism for **replacing xcactivitylog parse with
  proxy-observed ground-truth counts** correlated per-build.

### 4. `-cas-plugin-option session-id=…` is NOT forwarded in gRPC

Apple's `libToolchainCASPlugin.dylib` consumes the plugin option internally
and does not emit it as gRPC metadata or in request bodies. Proxy's
`metadata.FromIncomingContext(ctx)` on CAS RPCs returned only stock headers:

```
md=map[:authority:[localhost] content-type:[application/grpc]
user-agent:[grpc-swift-nio/1.14.1]]
```

In-band `session-id` correlation via `-cas-plugin-option` therefore does not
work. Peer-PID (point 3) is the viable correlation path.

## What's unresolved

### SwiftBuild IDE path doesn't translate every build setting the same way

For `xcodebuild` CLI builds, SwiftBuild translates
`COMPILATION_CACHE_REMOTE_SERVICE_PATH` into a `-cas-plugin-option
remote-service-path=…` flag on swiftc. For Xcode.app IDE builds, SwiftBuild
instead writes a per-package `.cas-config` file alongside the build
intermediates:

```
{"CASPath":"…/CompilationCache.noindex/plugin","PluginPath":"…/libToolchainCASPlugin.dylib"}
```

**`.cas-config` has no `remote-service-path` field.** Plugin reads this file
and only sees local CAS path + plugin dylib path; it never attempts a remote
connection to our proxy. IDE builds succeed from local cache alone (plugin
populates `CompilationCache.noindex` from its own prior writes), and
`-showBuildSettings` reports `COMPILATION_CACHE_REMOTE_SERVICE_PATH` is set,
yet proxy never sees a connection.

### Toolchain `OTHER_SWIFT_FLAGS` doesn't reach IDE builds

Experimentally confirmed by setting a sentinel string
(`BITRISE-SPIKE-SESSION-DEADBEEF42`) in `OverrideBuildSettings.OTHER_SWIFT_FLAGS`:

- CLI xcodebuild log: sentinel appears in every swiftc invocation.
- IDE build log: sentinel absent from compile commands even though
  `-showBuildSettings` resolves it.

Toolchain's `OverrideBuildSettings` works for core compile-cache keys
(`CLANG_ENABLE_COMPILE_CACHE`, `COMPILATION_CACHE_ENABLE_PLUGIN`,
`COMPILATION_CACHE_PLUGIN_PATH`, `SWIFT_ENABLE_COMPILE_CACHE`) in both
modes, but **`OTHER_SWIFT_FLAGS` propagation is CLI-only.**

### The trilemma

To make IDE + CLI + SPM all work simultaneously we'd need to satisfy three
things that currently conflict:

1. **IDE path** needs `OTHER_SWIFT_FLAGS = -cas-plugin-option
   remote-service-path=…` in a project-level xcconfig (toolchain's
   `OTHER_SWIFT_FLAGS` override is ignored in IDE mode).
2. **CLI path** translates `COMPILATION_CACHE_REMOTE_SERVICE_PATH` from
   toolchain `OverrideBuildSettings` into the swiftc flag automatically; if
   xcconfig *also* carries the equivalent `OTHER_SWIFT_FLAGS` line, CLI
   compilation fails with `Cannot setup CAS due to conflicting '-cas-*'
   options`.
3. **SPM reach** needs the setting to propagate via toolchain
   `OverrideBuildSettings` (xcconfig alone doesn't reach package targets).

Any two can be satisfied. All three at once needs a mechanism that lets us
know "we're in IDE context, inject the flag" vs "we're in CLI context,
don't". No build-system knob known today exposes that distinction.

## Not pursued

- **Toolchain code signing + notarization pipeline.** Not required for local
  installation under `~/Library/Developer/Toolchains/`. Would be required for
  `.pkg` distribution via a Developer ID identity.
- **In-band session-id via `-cas-plugin-option`** (not forwarded, see above).
- **Env-var correlation** via `OverrideBuildSettings` — Apple's plugin is
  closed, env-reading unverified. Peer-PID path (point 3) is strictly
  cheaper.
- **SWBBuildService lifetime across multiple `⌘B` presses in a single
  Xcode.app session.** Started to validate but blocked once the trilemma hit
  — IDE builds don't contact proxy, so Accept() never fires, so correlation
  evidence can't be gathered. Pending once remote connectivity is restored.

## Reference setup used

- Bundle: `/tmp/cas-tc2/io.bitrise.cas.xctoolchain/`
- Minimal `ToolchainInfo.plist`:

  ```xml
  <?xml version="1.0" encoding="UTF-8"?>
  <plist version="1.0">
  <dict>
    <key>Identifier</key><string>io.bitrise.cas.xctoolchain</string>
    <key>DisplayName</key><string>Bitrise CAS (Spike2)</string>
    <key>CompatibilityVersion</key><integer>2</integer>
    <key>CompatibilityVersionDisplayString</key><string>Xcode 15.0 or later</string>
    <key>OverrideBuildSettings</key>
    <dict>
      <key>COMPILATION_CACHE_ENABLE_CACHING</key><string>YES</string>
      <key>COMPILATION_CACHE_ENABLE_PLUGIN</key><string>YES</string>
      <key>COMPILATION_CACHE_PLUGIN_PATH</key>
        <string>/Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib</string>
      <key>COMPILATION_CACHE_REMOTE_SERVICE_PATH</key>
        <string>/var/folders/…/T/xcelerate-proxy.sock</string>
      <key>CLANG_ENABLE_COMPILE_CACHE</key><string>YES</string>
      <key>SWIFT_ENABLE_COMPILE_CACHE</key><string>YES</string>
    </dict>
  </dict>
  </plist>
  ```

- Install: `ln -sfn /tmp/cas-tc2/io.bitrise.cas.xctoolchain
  ~/Library/Developer/Toolchains/io.bitrise.cas.xctoolchain`
- Shadow Default toolchain's bins: `for f in
  /Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/bin/*;
  do ln -sfn "$f" "/tmp/cas-tc2/io.bitrise.cas.xctoolchain/usr/bin/$(basename
  $f)"; done`
- Also symlink `usr/{lib,libexec,include,share}` + `Developer` from Default
  toolchain.

- Fixture: `/tmp/cas-probe` (CasProbe XcodeGen project with `swift-collections`
  SPM dependency added via `project.yml`), source `App.swift` imports
  `Collections.OrderedSet`.

- Proxy spike binary (temporary instrumentation):
  `/tmp/bitrise-build-cache-spike-pid` built from the `feat/ide-enrichment-daemon`
  branch with `peerPIDListener` + `CAS-SPIKE-METADATA` interceptor logging in
  `internal/xcelerate/proxy/proxy.go`. The temp instrumentation was NOT
  committed and is removed as part of spike teardown.

## Next step options (future ticket)

1. **Ship toolchain for SPM + CI only.** `activate xcode` on CI installs the
   toolchain under `~/Library/Developer/Toolchains/` + sets
   `TOOLCHAINS=…` user default. Toolchain's `OverrideBuildSettings` wire
   everything (SwiftBuild CLI path translates
   `COMPILATION_CACHE_REMOTE_SERVICE_PATH` → swiftc flag). Local dev remains
   on the current `xcode link` xcconfig flow for IDE (SPM targets stay
   unreached locally).
2. **Resolve the IDE-swiftc-flag emission gap.** Dig into Apple's swift-build
   source to find what triggers SwiftBuild's `-cas-plugin-option
   remote-service-path=…` emission in CLI vs IDE and whether a new build
   setting (or a derived flag) can be set from toolchain
   `OverrideBuildSettings` to force the emission in IDE too. Candidates:
   `COMPILATION_CACHE_REMOTE_ENABLED`, something related to
   `COMPILATION_CACHE_REMOTE_SERVICE_SOCKET`, or a user-default that affects
   the IDE's SwiftBuild session.
3. **Proxy-side peer-PID session correlation** regardless of the above:
   productionize `peerPIDListener` + ancestry walk inside `internal/xcelerate/proxy/`
   so each new connection mints a session, aggregates proxy-observed counts,
   emits one PUT per session when the connection closes OR goes idle. Can
   replace or augment xcactivitylog-based enrichment introduced in PR #573
   once IDE builds reliably talk to the proxy.
4. **Investigate signed toolchain distribution** (Developer ID + notarize +
   `.pkg`) only after (1) is live and user feedback indicates GUI collateral
   worth gating via per-project `TOOLCHAINS` setting.
