# Xcode custom-toolchain approach for compile-cache reach

## Motivation

The pre-existing `xcode link` flow patches each `XCBuildConfiguration` in a
`.xcodeproj` to inherit a sibling xcconfig that wires Apple's stock LLVM CAS
plugin to our proxy. That reaches **app targets** on both CLI and IDE builds —
but it does **not** reach **SPM package targets**: SwiftBuild compiles packages
through its own build graph that never consults the app project's
`baseConfigurationReference`. For every CI customer whose iOS app pulls in a
non-trivial SPM dependency tree, SPM compilation was running uncached.

The custom-toolchain approach solves this at the toolchain layer: a bundle's
`OverrideBuildSettings` propagates to every target SwiftBuild compiles,
including SPM packages.

## Architecture

### Bundle layout

A thin bundle lives under `~/.bitrise-xcelerate/toolchain/io.bitrise.cas.xctoolchain/`
and is linked into the per-user Xcode discovery dir:

```
~/.bitrise-xcelerate/toolchain/io.bitrise.cas.xctoolchain/
    ToolchainInfo.plist          <- OverrideBuildSettings + identity
    usr/bin/<each-tool>          <- symlink to XcodeDefault.xctoolchain/usr/bin/<tool>
    usr/{lib,libexec,include,share}   <- symlinks to the Default's dirs
    Developer                    <- symlink to the Default's Developer dir

~/Library/Developer/Toolchains/io.bitrise.cas.xctoolchain  (symlink)
    -> ~/.bitrise-xcelerate/toolchain/io.bitrise.cas.xctoolchain
```

The bundle itself ships no binaries — every tool is a symlink to the Default
toolchain, so there's nothing to version-pin or rebuild per-Xcode-release. The
only Bitrise-authored file is `ToolchainInfo.plist`.

### Selecting the bundle

The xcconfig include block already written by `xcode link` is extended with a
single line:

```
TOOLCHAINS = io.bitrise.cas.xctoolchain
```

With `TOOLCHAINS=…` set, SwiftBuild resolves the named bundle for the project
and applies its `OverrideBuildSettings` to every build graph node — the app
target *and* every SPM package target.

### What `OverrideBuildSettings` carries

```
CLANG_ENABLE_COMPILE_CACHE            = YES
CLANG_ENABLE_MODULES                  = YES
COMPILATION_CACHE_ENABLE_CACHING      = YES
COMPILATION_CACHE_ENABLE_PLUGIN       = YES
COMPILATION_CACHE_PLUGIN_PATH         = <Xcode>/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib
COMPILATION_CACHE_REMOTE_SERVICE_PATH = <proxy socket>
OTHER_SWIFT_FLAGS                     = $(inherited) -cas-plugin-option remote-service-path=<proxy socket>
SWIFT_ENABLE_COMPILE_CACHE            = YES
```

The settings are the same six keys the xcconfig writes, plus
`COMPILATION_CACHE_REMOTE_SERVICE_PATH` and `OTHER_SWIFT_FLAGS`. The duplication
with the xcconfig is deliberate: the xcconfig still carries the whole set so a
build that does not pick up the toolchain (e.g. `TOOLCHAINS` stripped by a
CI task) still works for app targets.

`COMPILATION_CACHE_PLUGIN_PATH` is derived from the active Xcode developer dir
at install time (`xcode-select -p`-equivalent), **never** hardcoded to
`/Applications/Xcode.app` — so Xcode-beta and side-by-side installs resolve to
the right plugin.

## Current coverage matrix

| Context                              | App targets | SPM package targets |
|--------------------------------------|-------------|---------------------|
| CLI `xcodebuild` + toolchain         | yes         | yes                 |
| Xcode.app IDE + toolchain, Xcode 27.0 | yes        | no (see below)      |
| Xcode.app IDE + toolchain, Xcode 27.1 | yes        | yes (expected)      |

**CI is fully covered today.** Local IDE SPM reach is gated on a SwiftBuild
patch that lands in Xcode 27.1.

### Why IDE SPM reach lags

For CLI `xcodebuild` runs SwiftBuild translates
`COMPILATION_CACHE_REMOTE_SERVICE_PATH` into a `-cas-plugin-option
remote-service-path=…` flag on every swiftc invocation. For Xcode.app IDE runs
SwiftBuild instead writes a per-package `.cas-config` file alongside the build
intermediates, and the SwiftBuild version shipped in Xcode 27.0 omits the
`remote-service-path` field from that file. The plugin loads with local-only
CAS and never contacts the proxy for SPM compile units.

Apple's swift-build commit
[`14cb1b0989b8`](https://github.com/apple/swift-build) (merged 2026-07-24) adds
the remote path to the `.cas-config` `PluginOptions` emission. The fix is
expected in Xcode 27.1; when it ships, the CLI side needs no change — the
installed toolchain's `OverrideBuildSettings` already carries the setting and
the new SwiftBuild will read it. See
[`xcode-toolchain-roadmap.md`](xcode-toolchain-roadmap.md) for the follow-on
work.

## Known limitations

### `-cas-plugin-option session-id=…` not forwarded

Apple's `libToolchainCASPlugin.dylib` consumes the plugin option internally
and does **not** emit it as gRPC metadata or in request bodies. In-band
session-id correlation via `-cas-plugin-option` is therefore not viable.
Session correlation for per-build analytics uses **peer-PID observation** on
the proxy socket instead — see
[`xcode-toolchain-roadmap.md`](xcode-toolchain-roadmap.md).

## Install / uninstall flow

### `activate xcode`

1. Writes the override xcconfig at `~/.bitrise-xcelerate/xcode-app.xcconfig`
   with the six core settings + `TOOLCHAINS = io.bitrise.cas.xctoolchain`.
2. Installs the toolchain bundle at
   `~/.bitrise-xcelerate/toolchain/io.bitrise.cas.xctoolchain/`.
3. Symlinks the bundle into
   `~/Library/Developer/Toolchains/io.bitrise.cas.xctoolchain`.

Both toolchain steps are best-effort — a failure logs a warning and the rest
of activation proceeds. App targets will still cache (via xcconfig only); only
SPM reach is lost.

### `xcode link <project>`

Unchanged — patches `project.pbxproj` so each configuration inherits the
xcconfig. The toolchain install is independent and already in place after
`activate`.

### `xcode unlink <project>`

Unchanged — strips the xcconfig include block from the project. The toolchain
install stays in place; `TOOLCHAINS` setting in the xcconfig is only consumed
if `xcode link` has re-attached the include.

### `deactivate xcode`

Removes the discovery symlink first, then the bundle under
`~/.bitrise-xcelerate/toolchain/`. Non-fatal on failure.

## Signing

The toolchain bundle is **unsigned**. macOS accepts an unsigned toolchain for
per-user install under `~/Library/Developer/Toolchains/` without any
notarization or Developer ID prompt — the restriction only applies to
system-wide `/Library/Developer/Toolchains/` and to `.pkg` distribution.

Future `.pkg` distribution (so users can install the toolchain without the
CLI) would require a Developer ID identity + notarization; that path is **not
pursued** today.
