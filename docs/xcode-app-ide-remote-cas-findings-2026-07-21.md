# Xcode.app IDE remote CAS — findings 2026-07-21

## Context

Verified end-to-end whether `xcode-app enable` (launchctl `XCODE_XCCONFIG_FILE`)
and/or `xcode-app link` (per-project `#include? "<override>"`) actually engage
the remote CAS proxy (`xcelerate-proxy.sock`) for **Xcode.app IDE** builds on
macOS 26.4.1 / Xcode 26.6. Test project: IceCubesApp.

## Test matrix

All builds cold (`DerivedData`, `CompilationCache.noindex`, `ModuleCache.noindex`,
`SDKStatCaches.noindex` wiped; Xcode.app fully quit and relaunched between
runs so env inheritance is deterministic).

| Setup | Local plugin (CAS grows) | Remote CAS RPC to proxy | F2 enrichment PUT |
| --- | --- | --- | --- |
| `link` only, no env | **~950 MB** ✓ | **0 uploads** ✗ | ✓ |
| `enable` only, no link | 48 KB ✗ | 0 uploads ✗ | ✓ |
| `link` + `enable` (env) | **~950 MB** ✓ | **0 uploads** ✗ | ✓ |
| `xcodebuild` CLI + env inline | ~950 MB ✓ | **many uploads** ✓ | ✓ |

Only the CLI path opens the remote socket. **No IDE variant engages remote
CAS.** F2 enrichment PUTs fire regardless (xcactivitylog manifest processing
is independent of RPC).

## Root cause

Xcode's build system (`SwiftBuild` / `XCBBuild`) writes a `.cas-config` JSON
file per target build dir. The compile-cache plugin
(`libToolchainCASPlugin.dylib`) reads it. Grepping the shipped
`CoreBuildSystem.xcspec`:

```
COMPILATION_CACHE_ENABLE_CACHING
COMPILATION_CACHE_ENABLE_CACHING_DEFAULT
COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS
```

**Only three keys are recognised as build settings.** All others — including
`COMPILATION_CACHE_REMOTE_SERVICE_PATH`, `COMPILATION_CACHE_ENABLE_PLUGIN`,
`COMPILATION_CACHE_ENABLE_INTEGRATED_QUERIES`,
`COMPILATION_CACHE_ENABLE_DETACHED_KEY_QUERIES`,
`COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES`, `SWIFT_ENABLE_COMPILE_CACHE`,
`CLANG_ENABLE_COMPILE_CACHE` — pass through xcconfig settings resolution
(visible in `xcodebuild -showBuildSettings`) but are **silently dropped**
during `.cas-config` serialization.

Every `.cas-config` in an IDE build is:

```json
{"CASPath":"/Users/.../CompilationCache.noindex/builtin"}
```

No `RemoteService` field. Plugin never opens the socket. Local-only.

The plugin's own JSON schema (extracted from `libToolchainCASPlugin.dylib`
via `strings`) does support `RemoteService` (`remote-service-path`), so the
capability exists — but Xcode's build system has no build setting that maps
to it.

## What `enable` (launchctl setenv) actually does on macOS 26+

`XCODE_XCCONFIG_FILE` still layers into build settings for **`xcodebuild`
CLI** — settings resolve, plugin engages remote via a different config path
that CLI uses (`-cache-compile-job` + something env-inherited; the socket
path is not on the CLI swiftc argv).

For **Xcode.app IDE**, the env does propagate to the process (Xcode process
env has `XCODE_XCCONFIG_FILE` after `launchctl setenv` + relaunch) and
xcconfig settings resolve on paper, but `.cas-config` still contains only
`CASPath`. Net effect on IDE: nothing.

## What `link` (`#include? "<override>"`) actually does

Inserts the override's keys into the target's base xcconfig chain. IDE build
system resolves them into build settings. Then serializes `.cas-config` from
its allowlist of three known keys → same outcome as `enable`, minus one
subtlety: because the xcconfig also carries `CLANG_ENABLE_COMPILE_CACHE = YES`
/ `SWIFT_ENABLE_COMPILE_CACHE = YES` / `COMPILATION_CACHE_ENABLE_CACHING = YES`
in the include chain (`COMPILATION_CACHE_ENABLE_CACHING` IS one of the three
known keys), the local plugin engages. That's why `link` shows local CAS
growth and `enable` alone doesn't — with `enable` the settings live only in
env-layered xcconfig which the IDE processes differently than an actual
`#include` in a base xcconfig referenced by the target.

Net effect on IDE with `link`: **local compile-cache engages, remote does
not.** Real speedup on incremental IDE builds; no cross-machine reuse.

## Prior-session record

Reviewed all Claude Code session transcripts. **No prior session ever
demonstrated Xcode.app IDE (⌘B) engaging remote CAS.** The four sessions
that reference `xcelerate-cas-*` uploads either:

- Ran `xcodebuild` through the `activate xcode` PATH shim (wrapper mode, not
  IDE)
- Read old proxy log entries via `bash` and misattributed them
- Explicitly retracted the "IDE engages" claim ("The 11:48 delta was
  cross-session retry noise from an earlier wrapper run's queued CAS uploads
  that failed on a 20 s timeout batch cancellation")

The "Scenario B ✅" marks in the July 13 manual-testing docs used F2
enrichment PUT presence as proof — but F2 fires regardless of remote CAS
engagement. Hit rate on those BE rows was 0.

## Options to actually engage IDE remote CAS

Ranked by effort and reliability. None validated today.

1. **fswatch + patch `.cas-config`** — watch `Intermediates.noindex`, insert
   `"RemoteService"` into each `.cas-config` immediately after
   `WriteAuxiliaryFile` writes it, before the corresponding `SwiftCompile`
   task reads it. Race window exists but Xcode's dependency graph enforces
   WriteAuxiliaryFile → SwiftCompile ordering per target, so a fast watcher
   should win. Simplest to prototype.
2. **Patch once + `chflags uchg`** — build once, overwrite each
   `.cas-config` with our version, mark immutable. Next Xcode build's
   WriteAuxiliaryFile can't unlink → either errors or falls through.
   Depends on Xcode's error handling.
3. **Custom toolchain** at `~/Library/Developer/Toolchains/` selected via
   the Xcode Toolchains menu. Our swiftc adds
   `-cas-plugin-option remote-service-path=<socket>`. Heavy setup, but
   properly signed and stable.
4. **Fork `libToolchainCASPlugin.dylib`** — self-signed replacement that
   reads env `COMPILATION_CACHE_REMOTE_SERVICE_PATH` when `.cas-config`
   omits `RemoteService`. Blocked: `-cas-plugin-path` in IDE points at
   Xcode.app's internal path — can't be overridden without patching Xcode.
5. **VFS overlay** — swiftc supports `-ivfsoverlay`. Xcode doesn't pass
   that flag for `.cas-config` reads and there's no build setting to add
   one. Dead end from IDE side.
6. **Apple radar** — file the missing build setting
   (`COMPILATION_CACHE_REMOTE_SERVICE_PATH` needs to land in
   `CoreBuildSystem.xcspec`). Long lead time.

## What we shipped in PR #423 that these findings don't invalidate

- **F1/F2 enrichment** — F2 verified processing IDE builds today (BE rows
  posted via xcactivitylog manifest). F1's slim-emit + wrapper marker still
  applies to `xcodebuild` CLI wrapper mode.
- **4-path `-fdepscan-prefix-map`** — applies to both local and remote CAS
  key generation. Still correct for CLI mode where remote engages.
- **`link` mechanism** — real speedup on IDE via local plugin, even without
  remote.
- **`enable` mechanism** — still correct for `xcodebuild` CLI env
  inheritance path.

## What needs correction

- `xcode-app link` output text ("cache engages automatically on the next
  Xcode build") — implies remote engagement; only local engages for IDE.
- `xcode-app enable` output text ("on macOS 26+ … also run xcode-app link")
  — implies `link` fills the gap; it fills only the local half.
- `docs/xcode-app.md` — describes remote CAS as the outcome of `enable`; on
  macOS 26+ IDE that outcome is not achieved. Needs a warning up top.

## Reproducer

```sh
# Assumes bitrise-build-cache CLI installed and authenticated.
# Target: any Xcode project or workspace.

# Cold caches
osascript -e 'tell application "Xcode" to quit'
rm -rf ~/Library/Developer/Xcode/DerivedData/<PROJECT>-* \
       ~/Library/Developer/Xcode/DerivedData/CompilationCache.noindex \
       ~/Library/Developer/Xcode/DerivedData/ModuleCache.noindex \
       ~/Library/Developer/Xcode/DerivedData/SDKStatCaches.noindex

# Test A: link only, no env
launchctl unsetenv XCODE_XCCONFIG_FILE
bitrise-build-cache xcode-app link /path/to/YourApp.xcodeproj
# Relaunch Xcode.app; build; check:
find ~/Library/Developer/Xcode/DerivedData/<PROJECT>-*/Build/Intermediates.noindex \
  -name .cas-config -exec cat {} \;   # → {"CASPath":"..."} only, no RemoteService
du -sh ~/Library/Developer/Xcode/DerivedData/CompilationCache.noindex  # → hundreds of MB, local plugin on
tail -f ~/.local/state/xcelerate/logs/proxy-*-out.log  # → no "Upload xcelerate-cas-*" lines
```

Same repeated with env set and/or link removed produces the matrix above.
