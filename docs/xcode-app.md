# Xcode.app IDE remote build cache

`bitrise-build-cache xcode-app enable` routes Xcode.app (the GUI application,
⌘B / ▶) through Bitrise's xcelerate-proxy for remote CAS. It complements
`activate xcode`, which only affects command-line `xcodebuild` invocations.

macOS only. Xcode 27+ recommended (the underlying `libToolchainCASPlugin.dylib`
works on Xcode 26 too, but the retest that confirmed end-to-end remote CAS
was on Xcode 27 — see [`docs/xcode-app-ide-remote-cas-findings-2026-09-30.md`](xcode-app-ide-remote-cas-findings-2026-09-30.md)
for the mechanism and the "why").

## The SPM caveat — read this first

**IDE remote-CAS coverage is partial.** `XCODE_XCCONFIG_FILE` propagates into
Xcode-native targets, but **not** into Swift Package Manager package targets:
their build settings are resolved in a scope that does not inherit the env-var
override.

Practical consequence: for a workspace whose app target is Xcode-native and
whose packages are SPM (the common shape), `xcode-app enable` gives you remote
CAS for the app target's own compiles. The SPM packages compile against the
built-in local cache only, exactly as before.

For CI-style `xcodebuild` invocations there is no gap — command-line `KEY=VAL`
overrides do reach SPM package scope, and the wrapper installed by
`activate xcode` adds them automatically.

## What enable does

1. Reads the xcelerate proxy socket path from `~/.bitrise-xcelerate/config.json`
   (populated by `activate xcode`).
2. Writes `~/.bitrise-xcelerate/xcode-app.xcconfig` with the settings that turn
   on Xcode's CAS plugin and point it at the proxy socket.
3. Runs `launchctl setenv XCODE_XCCONFIG_FILE …` so the next Xcode.app launch
   picks up the override.
4. Registers a LaunchAgent at `~/Library/LaunchAgents/io.bitrise.build-cache.xcode-app-setenv.plist`
   so the env var persists across logout / reboot.
5. If a previous `XCODE_XCCONFIG_FILE` was already set, chains it in via
   `#include?` so the user's own override still applies.

Written xcconfig body:

```
// Bitrise Build Cache — Xcode.app IDE override
// Written by `bitrise-build-cache xcode-app enable`. Removed by `xcode-app disable`.
// Do not edit by hand.

#include? "<prior XCODE_XCCONFIG_FILE, if any>"

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

## Usage

```
bitrise-build-cache activate xcode              # if not already active
bitrise-build-cache xcode-app enable
```

If Xcode.app is already running, quit and relaunch it — `launchctl setenv`
only takes effect for processes launched after the call.

To turn it off:

```
bitrise-build-cache xcode-app disable
```

`disable` is idempotent and safe to run when nothing was ever enabled. It
removes the override xcconfig, boots out the LaunchAgent, deletes the plist,
and unsets `XCODE_XCCONFIG_FILE`. It does **not** stop the xcelerate-proxy —
the `xcodebuild` wrapper flow depends on it.

## Verify it worked

After enabling and relaunching Xcode:

```
launchctl getenv XCODE_XCCONFIG_FILE
# → /Users/<you>/.bitrise-xcelerate/xcode-app.xcconfig

cat ~/.bitrise-xcelerate/xcode-app.xcconfig
# → the settings block above

bitrise-build-cache doctor
# → xcode-app-override: ok (…, LaunchAgent …)
```

Then do a Product → Clean Build Folder and hit ⌘B. Xcode writes a metrics line
into the build log at the end:

```
CompilationCacheMetrics
note: 130 hits / 130 cacheable tasks (100%)
```

You can also tail the proxy log to see remote loads happen live:

```
tail -f ~/.local/state/xcelerate/logs/proxy-*.log
```

## Troubleshooting

- **`xcode-app-override` doctor check warns "file missing"**: someone deleted
  the xcconfig under your feet — re-run `enable`.
- **The proxy is not running**: enable does not start it. Run
  `bitrise-build-cache xcelerate start-proxy` (or restart via the standard
  xcelerate flow).
- **Metrics report 0 hits / 0 cacheable tasks**: the settings did not reach the
  target scope. For SPM package targets this is expected (see the caveat
  above). For Xcode-native targets, verify `XCODE_XCCONFIG_FILE` is set for
  the Xcode.app process (`launchctl getenv XCODE_XCCONFIG_FILE`) and that the
  path resolves.

## Further reading

- [Findings that unblocked this feature (2026-09-30)](xcode-app-ide-remote-cas-findings-2026-09-30.md)
- [Prior investigation (2026-07-21), retained for context](xcode-app-ide-remote-cas-findings-2026-07-21.md)
