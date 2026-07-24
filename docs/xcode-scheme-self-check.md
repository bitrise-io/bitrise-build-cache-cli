# Xcode Scheme Self-Check (Pre-Build `doctor`)

Run `bitrise-build-cache doctor` from your Xcode scheme's **Build → Pre-actions**, so every ⌘B / Run / Test verifies the local Build Cache setup *before* `swiftc` starts. Catches missing proxy, stale auth, or downed daemon before a full uncached build burns minutes.

For CLI + auth setup, read [`install.md`](install.md) first — this page assumes it's done and that you've run `bitrise-build-cache activate xcode` (which installs the `xcodebuild` wrapper and writes the `xcelerate` config).

---

## When to add this

You are:

- Driving builds through the `xcodebuild` CLI wrapper installed by `bitrise-build-cache activate xcode` (either directly, or via Xcode.app's Product menu when it's picked up the wrapper).
- Want a fast fail at build start when the cache setup drifts (daemon not running, PAT rotated, `xcelerate` config wiped).

Skip on Bitrise CI — the `xcodebuild` wrapper step already probes health there.

---

## Add the pre-action

1. In Xcode: **Product → Scheme → Edit Scheme…** (⌘<)
2. Left sidebar → **Build** → expand the arrow → **Pre-actions**.
3. **+** → **New Run Script Action**.
4. **Provide build settings from:** pick the primary target of the scheme (needed to inherit the target's environment).
5. **Shell:** `/bin/sh` (default).
6. Paste the body below.
7. **Close** to save. The `.xcscheme` XML now contains the action — commit it if the scheme is shared across the team.

### Run Script body

```sh
# Bitrise Build Cache — scheme pre-build self-check
set -eu

LOG="${TMPDIR:-/tmp}/bitrise-build-cache-doctor.log"

# Xcode strips PATH down to a minimal set. Extend to common CLI install dirs.
export PATH="$HOME/.bitrise/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"

if ! command -v bitrise-build-cache >/dev/null 2>&1; then
    # CLI not installed — nothing to check. Silent no-op.
    exit 0
fi

if bitrise-build-cache doctor --no-update-check --no-backend-probe >"$LOG" 2>&1; then
    exit 0
fi

# doctor exited non-zero → at least one check reported an error.
# Surface the failure via a modal so the developer can't miss it.
osascript <<APPLESCRIPT
display alert "Bitrise Build Cache: setup check failed" \
    message "The pre-build health check reported errors. Your build will run without the remote cache.

Fix and retry:

    bitrise-build-cache doctor --fix

Full log: $LOG" \
    as critical
APPLESCRIPT

exit 1
```

**Copy tip:** the alert message is a literal `osascript` string — keep the blank lines between `errors.`, `Fix and retry:`, `    bitrise-build-cache …`, and `Full log:`; they render as paragraph breaks in the dialog.

---

## What each flag does

- `--no-update-check` — skips the GitHub release lookup. Pre-actions run on every build; you don't want a network call added to every ⌘B just to check for a new CLI version.
- `--no-backend-probe` — skips the Build Cache backend auth probe (sentinel KV `PUT`). Same reason: keep the check local + fast.

`doctor` still verifies: auth token presence, keychain access, `xcelerate` config, daemon socket, ccache helper, log dirs.

---

## Behaviour notes (read before shipping to a team)

- **Xcode pre-actions do not hard-abort the build on non-zero exit.** The `osascript` modal is the reliable way to surface the failure to the developer — they read it, click OK, and the build then proceeds *without* the cache. If your team wants a hard block, add a Build Phase Run Script at the top of each target that fails on a marker file written by the pre-action. See [Hard block](#hard-block-optional) below.
- **Output is not streamed to Xcode's Report navigator.** That's why the script tees output to `$LOG` and points the modal at the file — the developer opens it in Console/Terminal to see the exact `doctor` lines.
- **PATH.** Xcode pre-actions inherit a stripped PATH. The script extends it to the three common CLI install roots — `$HOME/.local/bin` (`installer.sh` default), `/opt/homebrew/bin` (Homebrew Apple Silicon), `/usr/local/bin` (Homebrew Intel).
- **Silent no-op when CLI missing.** If a teammate hasn't installed the CLI yet, the pre-action exits `0` rather than yelling at them mid-build. The absence is a setup issue, not a build issue — `docs/install.md` covers onboarding.

---

## Hard block (optional)

If a soft warning isn't enough — for example, you have a strict policy that no build may proceed without the cache — combine the pre-action with a per-target Build Phase Run Script that reads a marker file.

**Pre-action** (replace the `exit 1` at the bottom):

```sh
touch "${TMPDIR:-/tmp}/bitrise-build-cache-doctor.failed"
exit 1
```

Also add, at the top of the script, a clean-up when doctor succeeds:

```sh
rm -f "${TMPDIR:-/tmp}/bitrise-build-cache-doctor.failed"
```

**Build Phase Run Script** (add as the *first* Run Script phase in each target's Build Phases):

```sh
if [ -f "${TMPDIR:-/tmp}/bitrise-build-cache-doctor.failed" ]; then
    echo "error: Bitrise Build Cache doctor failed; run 'bitrise-build-cache doctor --fix'."
    exit 1
fi
```

Now Xcode aborts compilation at the first target that sees the marker.

---

## Verify

1. Break the setup on purpose (e.g. `bitrise-build-cache daemon down`).
2. ⌘B a target that uses the scheme.
3. The modal should appear within a second. Dismiss it.
4. `cat "${TMPDIR:-/tmp}/bitrise-build-cache-doctor.log"` shows the exact `doctor` output including the `↳ rerun with --fix to repair` hint on fixable items.
5. `bitrise-build-cache doctor --fix` restores the setup; next ⌘B should pass silently.

---

## Team-wide rollout

The pre-action lives inside the `.xcscheme` file at
`YourProject.xcodeproj/xcshareddata/xcschemes/<Scheme>.xcscheme`. Commit that file to the repo and every teammate inherits the check on their next `git pull`.

Only shared schemes work for this — per-user schemes (`xcuserdata/…`) aren't checked in. If your team runs on per-user schemes, either promote them to shared or ship the snippet in your project README as copy-paste.

---

## Troubleshooting

| Symptom | First thing to try |
| --- | --- |
| Modal never appears; build runs without cache regardless | `command -v bitrise-build-cache` returns empty inside the pre-action's PATH. Confirm the install path is in the extended `PATH` line (`$HOME/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`). |
| Modal appears on every build even after `doctor --fix` | Doctor may report a WARN, not just ERROR — but the current snippet only escalates on non-zero exit (`doctor` exits `0` on WARN). Re-run `bitrise-build-cache doctor` in Terminal to read the exact state. |
| `$LOG` file grows large over time | Rotate: replace `>"$LOG"` with `>"$LOG.tmp" && mv "$LOG.tmp" "$LOG"` (single overwrite per run — this is already the effective behaviour of `>` in `sh`). |
| Pre-action runs but `osascript` blocks headlessly on CI VMs | The pre-action is intentionally scoped to Xcode.app GUI. For `xcodebuild` CLI builds, the CLI wrapper step handles health checks — remove or guard the alert behind `[ -t 0 ]` if a scheme runs in both contexts. |

Open issues at [github.com/bitrise-io/bitrise-build-cache-cli](https://github.com/bitrise-io/bitrise-build-cache-cli/issues).
