# Per-project scoping

The CLI reads an optional per-project marker file so a repo can advertise that
it participates in the build-cache flow. Presence is the only signal: no marker
→ the CLI behaves exactly as before; marker present → the doctor reports it.

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

## What this file used to describe (retracted)

An earlier iteration of ACI-5357 used the marker to route between multiple
per-workspace credentials stored on the same host, and carried `push` and
per-tool override fields. That design was rejected — the machine-wide
credential story stays: one credential per host, plus the presence-only
opt-in signal documented above.
