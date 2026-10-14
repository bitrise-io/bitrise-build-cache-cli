# Run xcodebuild from the terminal

The `bitrise-build-cache xcode build` and `bitrise-build-cache xcode test` subcommands
run xcodebuild through the Bitrise Build Cache wrapper without you having to remember
the argv shape (workspace / scheme / destination / configuration). They are for local
development on macOS — CI keeps calling the wrapper directly with the full argv.

## Discovery

Each run resolves an invocation spec in three steps, in order:

1. **Repo-local config file.** `<repoRoot>/.bitrise-build-cache/xcode-build.json` for
   `xcode build`, or `xcode-test.json` for `xcode test`. If the file has a complete
   spec, it wins.
2. **DerivedData scan.** The wrapper looks at the most recent Xcode build in the
   local DerivedData tree and fills in workspace / project / scheme / configuration
   from what it finds.
3. **Interactive prompt.** Anything still missing is asked on the terminal.

A successful resolution rewrites the config file so subsequent runs skip discovery
and prompt entirely.

## Committed config schema

```json
{
  "workspace": "MyApp.xcworkspace",
  "project": "MyApp.xcodeproj",
  "scheme": "MyApp",
  "configuration": "Debug",
  "destination": "generic/platform=iOS Simulator",
  "extraArgs": ["-quiet"]
}
```

Set exactly one of `workspace` / `project`. If both are set, `workspace` wins on save.
`configuration` is optional. `extraArgs` is appended to the xcodebuild argv verbatim.

## Flags

- `--reconfigure` — delete any cached config file, re-run discovery, and prompt for
  anything still missing.
- `--codesign` — enable codesigning. Off by default. When off, the wrapper appends
  `CODE_SIGNING_ALLOWED=NO CODE_SIGN_IDENTITY= CODE_SIGNING_REQUIRED=NO` so local
  builds don't need signing credentials.

Anything after `--` is passed straight through to `xcodebuild`.

## Examples

```bash
# Guided first-run: resolves workspace/scheme/destination, persists to
# .bitrise-build-cache/xcode-build.json, runs `xcodebuild build`.
bitrise-build-cache xcode build

# Reuses the persisted spec.
bitrise-build-cache xcode test

# Forget the persisted spec and reconfigure interactively.
bitrise-build-cache xcode build --reconfigure

# Pass extra xcodebuild flags for this run only (not persisted).
bitrise-build-cache xcode build -- -quiet -showBuildTimingSummary
```

If the config is incomplete and the terminal is non-interactive (no TTY, e.g. piped
output), the command exits with an error naming the config path so you can hand-edit
the missing fields.

## Routing Xcode.app IDE builds (⌘B / ▶) through the proxy

`xcode build` / `xcode test` run xcodebuild via the wrapper, which sets the right
build settings on every invocation. **Xcode.app IDE builds bypass the wrapper** — ⌘B
calls `swiftc` directly via the `SwiftBuild` service.

`activate xcode` writes an override xcconfig at `~/.bitrise-xcelerate/xcode-app.xcconfig`
as a side effect. `xcode link <project>` wires it into the project's
`baseConfigurationReference`; after that, ⌘B / ▶ routes through the xcelerate-proxy for
remote CAS the same way the CLI wrapper does. macOS only.

```bash
bitrise-build-cache activate xcode                  # writes override xcconfig
bitrise-build-cache xcode link <path-to-project>    # wires it into pbxproj
```

Accepts a `.xcodeproj` or `.xcworkspace`. For a workspace, every referenced
`.xcodeproj` is processed; `Package.swift` references and nested project-of-project
references are skipped. Idempotent — re-run after a path change picks up cleanly.

If Xcode.app has the project loaded already, quit and relaunch — the include is
not re-read for an already-loaded project.

### SPM caveat

**IDE remote-CAS coverage is partial.** `xcode link` propagates the override to
Xcode-native targets but **not** to Swift Package Manager package targets: their
build settings resolve in a scope that does not pick up a sibling xcconfig or a
`baseConfigurationReference` from the pbxproj. SPM package targets in an IDE build
hit only the local compilation cache.

For CI-style `xcodebuild` invocations there is no gap — command-line `KEY=VAL`
overrides reach SPM package scope, and the wrapper adds them automatically.

### Verifying

After `activate xcode` + `xcode link` + relaunching Xcode:

```bash
cat ~/.bitrise-xcelerate/xcode-app.xcconfig
# → the settings block (COMPILATION_CACHE_* + OTHER_SWIFT_FLAGS)

bitrise-build-cache doctor
# → xcode-app-override: ok (…/xcode-app.xcconfig, proxy socket …)
```

Then Product → Clean Build Folder → ⌘B. Xcode logs a metrics line at the end:

```
CompilationCacheMetrics
note: 130 hits / 130 cacheable tasks (100%)
```

Tail the proxy log to see remote loads live:

```bash
tail -f ~/.local/state/xcelerate/logs/proxy-*.log | grep xcelerate-cas
```

### Reverting

```bash
bitrise-build-cache xcode unlink <path-to-project>   # per project
bitrise-build-cache deactivate xcode                 # removes override xcconfig
```

`unlink` strips the marker block from every xcconfig `link` touched and removes a
sibling file whose only remaining content was the marker. `baseConfigurationReference`
set by `link` is left in place — pbxproj offers no way to tell "user had it" from
"we set it".

### Troubleshooting

- **`xcode-app-override` doctor check warns "override xcconfig missing"** — re-run
  `bitrise-build-cache activate xcode`.
- **Proxy not running** — `activate xcode` does not start it. Run
  `bitrise-build-cache xcelerate start-proxy`.
- **Metrics report 0 hits / 0 cacheable tasks** — settings didn't reach the target
  scope. For SPM package targets this is expected (see caveat). For Xcode-native
  targets, re-run `xcode link <path>` and confirm the sibling or base xcconfig
  contains the marker-fenced `#include?` line.
