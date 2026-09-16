# Per-project scoping

The CLI reads an optional per-project marker file so a repo can advertise that
it participates in the build-cache flow. Combined with a machine-wide mode, the
marker can also gate whether cache work happens at all: an opt-in machine only
touches the cache for projects that carry a marker.

## The marker file

Lives at the repo root (or any ancestor of the current working directory — the
CLI walks up until it finds one or hits the filesystem root):

```
.bitrise-build-cache.json
```

Constant: `paths.ProjectMarkerFilename`.

## Schema

The marker is a JSON object. It currently has no defined fields; an empty `{}`
is the canonical content:

```json
{}
```

Unknown fields are silently ignored (`json.Unmarshal` drops keys not on the
struct), so a marker carrying extra keys — e.g. a legacy `workspace` field from
an earlier iteration of the design — still parses. A malformed marker surfaces
as `StateError` in the doctor; activator paths treat an unreadable marker the
same as no marker at all.

## How the CLI reads the marker

- `internal/config/common.WalkUpFindMarker(cwd, osProxy)` walks up from the
  starting directory until it finds a `.bitrise-build-cache.json`, returning
  `("", nil, nil)` if none exists. Symlinks are not resolved.
- `internal/config/common.ReadProjectMarker(path, osProxy)` parses a specific
  path. Missing file → `(nil, nil)`. Malformed JSON → error.
- The doctor's `project-scope` check runs the walk-up from the current directory
  and reports either "no marker found" or "marker at <path>".

## Mode

The CLI remembers a machine-wide `project_mode` in
`~/.bitrise/build-cache/config.json`:

```json
{
  "project_mode": "opt-in"
}
```

Two values are accepted:

- `always` (default) — every activated tool caches regardless of the marker.
- `opt-in` — every activated tool gates its runtime work on the marker walk-up.
  When no marker is found, Gradle skips `buildCache` wiring, the Xcode wrapper
  skips proxy startup, the Bazel credential helper returns empty headers, and
  the ccache storage helper answers GET with miss / PUT with push-disabled.

### Setting the mode

Every `activate` command takes `--project-mode`:

```bash
bitrise-build-cache activate gradle --project-mode opt-in
bitrise-build-cache activate xcode  --project-mode always
```

Passing a value both switches this activation and persists the choice. Leaving
the flag empty inherits the stored value; a fresh machine defaults to `always`.

The interactive wizard exposes the same choice:

```bash
bitrise-build-cache activate --interactive
```

### Reactivation is the switch

Each tool bakes the effective mode into its activation artifact — the Gradle
init script, the xcelerate `config.json`, the ccache sidecar, and the
`.bazelrc`. Flipping the mode requires re-running `activate` so the new value
lands in each artifact. The ccache activator additionally restarts the storage
helper when it detects the delta so the next build reflects the new setting.

### Per-tool behavior

| Tool | Opt-in gate location | Effect when no marker found |
| --- | --- | --- |
| Gradle | init script's `settingsEvaluated` block, `java.io.File` walk-up | Prints `[bitrise-build-cache] project-mode=opt-in, no marker found ...` and returns before `buildCache`/analytics wiring runs |
| Xcode | `xcodebuild` wrapper, before proxy startup | Cache disabled with reason `project-mode=opt-in`; reuses the existing "cache disabled" analytics-only path |
| Bazel | Credential helper (`bitrise-build-cache get`) | Returns `{"headers":{}}`; every RPC's auth header is empty so the RPC 401s and Bazel falls back to no-cache |
| ccache | Storage helper's request processor | GET → miss (0-byte payload), PUT → push-disabled; the daemon does not touch the cache backend |
| React Native | Threaded through to Gradle + Xcode + ccache | Combination of the above |

### Bazel: reactivation reload

The credential helper reads the mode from `~/.bitrise/build-cache/config.json`
directly. A mid-session mode flip therefore takes effect on the next `bazel`
invocation.

## What this file used to describe (retracted)

An earlier iteration used the marker to route between multiple per-workspace
credentials stored on the same host, and carried `push` and per-tool override
fields. That design was rejected — the machine-wide credential story stays: one
credential per host, plus the presence-only opt-in signal documented above.
