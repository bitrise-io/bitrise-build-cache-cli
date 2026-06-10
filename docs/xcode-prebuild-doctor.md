# Xcode pre-build health check

A short health check that runs every time a developer hits ⌘B in Xcode. Catches missing proxies / stale credentials before the build burns minutes producing a no-cache result.

The check is implemented as a **scheme pre-action** that shells out to `bitrise-build-cache doctor`. Two install paths:

- **Recommended:** [the CLI subcommand](#one-command-install) — one shot, idempotent, scriptable.
- **Manual:** [the Xcode UI path](#manual-install-via-xcode-ui) — copy-paste into the scheme editor. Useful when you want to see / tweak the script before committing it.

Once installed, the action lives in `.xcodeproj/xcshareddata/xcschemes/<scheme>.xcscheme` under `<BuildAction>/<PreActions>`. The file is normally checked in alongside the project, so committing it propagates the check to all teammates.

## One-command install

```bash
bitrise-build-cache install-prebuild \
  --project ~/dev/MyApp/MyApp.xcodeproj \
  --scheme MyApp
```

Re-running is a no-op (idempotent). To remove:

```bash
bitrise-build-cache install-prebuild \
  --project ~/dev/MyApp/MyApp.xcodeproj \
  --scheme MyApp \
  --remove
```

Restart Xcode (or close + reopen the project) so the IDE picks up the modified scheme.

## Manual install via Xcode UI

1. **Product → Scheme → Edit Scheme…** (or ⌘<).
2. Select **Build** on the left.
3. Expand **Pre-actions**.
4. Click **+** at the bottom-left → **New Run Script Action**.
5. **Provide build settings from:** pick your main target (so `${SRCROOT}` etc. are available — not strictly required for our script, but matches Apple's convention).
6. Paste the body below into the script editor:

```bash
# bitrise-build-cache-prebuild-marker-v1
if ! command -v bitrise-build-cache >/dev/null 2>&1; then
  echo "warning: bitrise-build-cache not installed; skipping health check"
  exit 0
fi
bitrise-build-cache doctor --no-update-check
```

7. Close the editor. Xcode auto-saves the scheme.

## What runs at build-time

`bitrise-build-cache doctor --no-update-check` runs every doctor check except the GitHub-release version comparison (skipped to keep pre-build fast and avoid a network call). It exits non-zero when overall state is **error** (e.g. missing credentials, dead proxy pid, log dir unwritable).

Note on Xcode behaviour: scheme pre-actions in Xcode do **not** by default abort the build on a non-zero exit. The script's output still appears in the Report Navigator under the "Run Script" step, so developers see the doctor warnings before swiftc runs. If you need hard-fail behaviour, prefer a target-level **Build Phase Run Script** (more invasive than the scheme tweak; not what this command sets up).

## Caveats

- The check shells out to `bitrise-build-cache` on the developer's `PATH`. If the binary isn't installed yet, the script logs a warning and exits 0 — better than blocking the first build after onboarding.
- The injected XML lives in `xcshareddata/xcschemes/`, which is typically committed. Coordinate with your team before pushing the change so reviewers know what to look for.
- Scheme files are owned by Xcode. The CLI uses targeted regex insertion (not a full XML round-trip) to keep the git diff minimal — `<PreActions>` block + closing tag, nothing else moves.
- Re-running install when the marker is already present is a no-op. If the script body changes in a future CLI version, the marker version (`-v1`) bumps and a follow-up `install-prebuild` rewrites the action.

## Implementation notes

Code lives in `internal/xcodescheme/prebuild.go` (Install/Uninstall/ResolveSchemePath) and `cmd/xcode/install_prebuild.go` (cobra wrapper).

The marker line `bitrise-build-cache-prebuild-marker-v1` is what makes idempotency + selective uninstall safe: pre-existing customer `<PreActions>` blocks without our marker are left alone.
