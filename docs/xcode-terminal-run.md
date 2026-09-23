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
