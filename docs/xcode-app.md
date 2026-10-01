# Xcode.app IDE remote build cache

Route Xcode.app (the GUI application, ⌘B / ▶) through Bitrise's xcelerate-proxy
for remote CAS. Two commands, done:

```
bitrise-build-cache activate xcode                       # writes the override xcconfig
bitrise-build-cache xcode-app link <path-to-project>     # wires it into the pbxproj
```

`activate xcode` writes `~/.bitrise-xcelerate/xcode-app.xcconfig` as a side
effect of normal activation. `xcode-app link` patches every
`XCBuildConfiguration` in the project (or every `.xcodeproj` referenced by a
`.xcworkspace`) so that override is reachable through the configuration's
`baseConfigurationReference` chain.

macOS only. Xcode 27+ recommended (the underlying `libToolchainCASPlugin.dylib`
works on Xcode 26 too, but the retest that confirmed end-to-end remote CAS was
on Xcode 27 — see
[`docs/xcode-app-ide-remote-cas-findings-2026-09-30.md`](xcode-app-ide-remote-cas-findings-2026-09-30.md)
for the mechanism and the "why").

## The SPM caveat — read this first

**IDE remote-CAS coverage is partial.** `link` propagates the override to
Xcode-native targets, but **not** to Swift Package Manager package targets:
their build settings are resolved in a scope that does not pick up a sibling
xcconfig or a `baseConfigurationReference` the pbxproj patcher adds.

Practical consequence: for a workspace whose app target is Xcode-native and
whose packages are SPM (the common shape), the linked project gets remote CAS
for the app target's own compiles. The SPM packages compile against the
built-in local cache only.

For CI-style `xcodebuild` invocations there is no gap — command-line `KEY=VAL`
overrides do reach SPM package scope, and the wrapper installed by
`activate xcode` adds them automatically.

## What the two commands do

`activate xcode` (standard activation plus one side effect):

1. Does everything it already did — writes `~/.bitrise-xcelerate/config.json`,
   installs the `xcodebuild` / `xcrun` wrappers, resolves auth, etc.
2. Writes `~/.bitrise-xcelerate/xcode-app.xcconfig` with the settings that turn
   on Xcode's CAS plugin and point it at the xcelerate-proxy socket.

Written xcconfig body:

```
// Bitrise Build Cache — Xcode.app IDE override
// Written by `bitrise-build-cache activate xcode`. Removed by `deactivate xcode`.
// Do not edit by hand.

CLANG_ENABLE_COMPILE_CACHE = YES
CLANG_ENABLE_MODULES = YES
COMPILATION_CACHE_ENABLE_CACHING = YES
COMPILATION_CACHE_ENABLE_PLUGIN = YES
COMPILATION_CACHE_PLUGIN_PATH = /Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib
COMPILATION_CACHE_REMOTE_SERVICE_PATH = /var/folders/…/T/xcelerate-proxy.sock
OTHER_SWIFT_FLAGS = $(inherited) -cas-plugin-option remote-service-path=/var/folders/…/T/xcelerate-proxy.sock
SWIFT_ENABLE_COMPILE_CACHE = YES
```

The template deliberately **omits** `COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES`:
SwiftBuild's `CompilationCachingConfigFileTaskProducer` has a FIXME that bails
(no `.cas-config` written) when both `REMOTE_SERVICE_PATH` and
`SUPPORTED_LANGUAGES` are set. See the findings doc.

`xcode-app link <path>` wires the override into pbxproj. For each
`XCBuildConfiguration` in the project:

- If `baseConfigurationReference` is already set, a marker-fenced
  `#include? "<override>"` block is appended to that xcconfig. Idempotent —
  re-running replaces the block cleanly, so a bumped override path is picked up
  on the next run.
- If `baseConfigurationReference` is NOT set, a sibling
  `.bitrise-build-cache.xcconfig` is written next to the `.xcodeproj`, the
  configuration's `baseConfigurationReference` is set to it, and the override
  is `#include?`-ed from there.

## Linking a project

`xcode-app link <path>` accepts either a `.xcodeproj` or a `.xcworkspace`. For
a workspace, every referenced `.xcodeproj` is processed; `Package.swift`
references and nested project-of-project references are skipped (SPM package
scope is out of scope, see the caveat above).

If Xcode.app has the project loaded already, quit and relaunch it — the include
does not take effect for an already-loaded project.

## Verify it worked

After `activate xcode` + `link` + relaunching Xcode:

```
cat ~/.bitrise-xcelerate/xcode-app.xcconfig
# → the settings block above

bitrise-build-cache doctor
# → xcode-app-override: ok (…/xcode-app.xcconfig, proxy socket …)
```

Then do a Product → Clean Build Folder and hit ⌘B. Xcode writes a metrics line
into the build log at the end:

```
CompilationCacheMetrics
note: 130 hits / 130 cacheable tasks (100%)
```

You can also tail the proxy log to see remote loads happen live — look for the
`Load with key xcelerate-cas-*` pattern:

```
tail -f ~/.local/state/xcelerate/logs/proxy-*.log | grep xcelerate-cas
```

## Reverting

```
bitrise-build-cache xcode-app unlink <path-to-project>   # per project
bitrise-build-cache deactivate xcode                     # removes override xcconfig
```

`unlink` strips the marker block from every xcconfig `link` touched, and
removes a sibling file whose only remaining content was the marker block.
`baseConfigurationReference` set by `link` is left in place — the pbxproj
offers no way to distinguish "the user already had this" from "we set it".

`deactivate xcode` removes `~/.bitrise-xcelerate/` wholesale, including the
override xcconfig written by activation.

## Troubleshooting

- **`xcode-app-override` doctor check warns "override xcconfig missing"**:
  something deleted the file under your feet — re-run
  `bitrise-build-cache activate xcode`.
- **The proxy is not running**: `activate xcode` does not start it. Run
  `bitrise-build-cache xcelerate start-proxy` (or restart via the standard
  xcelerate flow).
- **Metrics report 0 hits / 0 cacheable tasks**: the settings did not reach the
  target scope. For SPM package targets this is expected (see the caveat
  above). For Xcode-native targets, re-run `xcode-app link <path>` and confirm
  the sibling or base xcconfig actually contains the marker-fenced
  `#include?` line.

## Further reading

- [Findings that unblocked this feature (2026-09-30)](xcode-app-ide-remote-cas-findings-2026-09-30.md)
- [Prior investigation (2026-07-21), retained for context](xcode-app-ide-remote-cas-findings-2026-07-21.md)
