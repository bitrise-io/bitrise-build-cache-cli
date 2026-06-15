# Use the Bitrise Build Cache from Xcode.app (GUI builds)

A one-page guide for enabling the compile cache when you press ▶ in
Xcode.app. The default `xcodebuild` wrapper only kicks in for command-line
builds — this page covers the GUI case.

For the underlying CLI install + auth, see
[`docs/install.md`](install.md) first.

---

## TL;DR

```sh
# 1) one-time CLI + auth setup (see docs/install.md)
brew install bitrise-io/bitrise-build-cache/bitrise-build-cache
export BITRISE_BUILD_CACHE_AUTH_TOKEN="<PAT>"
export BITRISE_BUILD_CACHE_WORKSPACE_ID="<workspace-slug>"

# 2) configure the Xcode compile cache + start the proxy daemon
bitrise-build-cache activate xcode
bitrise-build-cache daemon install
bitrise-build-cache daemon up

# 3) enable the override for Xcode.app GUI builds
bitrise-build-cache xcode-app enable

# 4) relaunch Xcode, then build any target
```

macOS only. Linux + Windows don't ship Xcode.

---

## How it works

Xcode.app drives its build planner through `XCBBuildService` — the
`xcodebuild` wrapper is bypassed for GUI builds. Apple exposes a single
override slot, `XCODE_XCCONFIG_FILE`, that sits above per-target / per-base
xcconfig but below `xcodebuild` CLI args.

`xcode-app enable` does three things:

1. Writes an override `xcconfig` to
   `~/.bitrise-xcelerate/xcode-app.xcconfig` containing
   `COMPILATION_CACHE_REMOTE_SERVICE_PATH` plus the
   `COMPILATION_CACHE_ENABLE_*` / `SWIFT_*` / `CLANG_*` keys.
2. Runs `launchctl setenv XCODE_XCCONFIG_FILE <path>` so processes spawned
   from the current GUI session inherit the override.
3. Installs a one-shot LaunchAgent
   (`~/Library/LaunchAgents/io.bitrise.build-cache.xcode-app-setenv.plist`)
   that re-applies the `setenv` on every login, so the override survives
   logout.

The xcelerate-proxy daemon is also installed + started so Xcode.app has a
socket to dial.

---

## Prerequisites

| Step                                             | Why                                                                                                        |
| ------------------------------------------------ | ---------------------------------------------------------------------------------------------------------- |
| `bitrise-build-cache --version` works            | Confirms the CLI is on `$PATH`. See [`docs/install.md`](install.md).                                       |
| Auth env vars exported                           | `BITRISE_BUILD_CACHE_AUTH_TOKEN` + `BITRISE_BUILD_CACHE_WORKSPACE_ID`. See [post-install](install.md#post-install). |
| `bitrise-build-cache activate xcode` has run     | Writes `~/.bitrise-xcelerate/config.json` containing the proxy socket path that `xcode-app enable` reads.  |
| `bitrise-build-cache daemon install && daemon up` | Registers + starts the xcelerate-proxy service via launchd so Xcode.app finds it on first dial.            |

`xcode-app enable` errors with a clear hint if any of these are missing.

---

## Enable

```sh
bitrise-build-cache xcode-app enable
```

Output looks like:

```
✓ Wrote override xcconfig: /Users/<you>/.bitrise-xcelerate/xcode-app.xcconfig
✓ Set XCODE_XCCONFIG_FILE via launchctl (LaunchAgent: …/io.bitrise.build-cache.xcode-app-setenv.plist)
✓ Proxy socket: /var/folders/.../xcelerate-proxy.sock
! Xcode is currently running (pid [1234]). Quit and relaunch Xcode to pick up the cache override.
```

> **Relaunch Xcode after enable.** `launchctl setenv` only reaches processes
> launched *after* it ran. Already-open Xcode windows pick up the override
> only on the next launch. Use ⌘Q (not just close the window).

---

## Verify the override is in effect

After relaunching Xcode, three quick checks:

```sh
# 1. The env is set in the GUI session:
launchctl getenv XCODE_XCCONFIG_FILE

# 2. The proxy is running:
bitrise-build-cache doctor

# 3. Build any target. The doctor output should show the proxy has been
#    dialled at least once (logs under ~/.local/state/xcelerate/logs/).
```

If `launchctl getenv` prints nothing, the LaunchAgent didn't bootstrap —
re-run `xcode-app enable` and check the output for the LaunchAgent error.

If `doctor` reports the proxy is not running, run
`bitrise-build-cache daemon up`. The override expects the proxy to be
listening — Xcode silently skips the cache if the socket isn't there.

---

## Chaining an existing `XCODE_XCCONFIG_FILE`

If you (or your tooling) already set `XCODE_XCCONFIG_FILE` to a custom
xcconfig, `enable` captures the existing value and **chains it** into our
override via `#include` at the top of the file:

```text
// Bitrise Build Cache — Xcode.app override
// Written by `bitrise-build-cache xcode-app enable`.

#include "/Users/<you>/path/to/your/Base.xcconfig"

COMPILATION_CACHE_ENABLE_DETACHED_KEY_QUERIES = YES
...
```

Your settings keep winning where they don't collide with the
`COMPILATION_CACHE_*` keys. `disable` restores the prior value into
`launchctl` so re-enabling later picks up the same chain.

> **List settings** (`OTHER_CFLAGS`, `OTHER_LDFLAGS` etc.) — none today, but
> if a future override ever appends to one, use `$(inherited)` in the chained
> xcconfig so your values aren't dropped.

---

## Disable

```sh
bitrise-build-cache xcode-app disable
```

Reverses every step:

- `launchctl bootout` + remove the LaunchAgent plist.
- `launchctl unsetenv XCODE_XCCONFIG_FILE` — or, if a prior path was chained
  in at enable time, `launchctl setenv` back to it.
- Remove our override xcconfig from `~/.bitrise-xcelerate/`.

Idempotent — safe to run when nothing is enabled. Does **not** stop the
xcelerate-proxy daemon; use `bitrise-build-cache daemon down` for that.

---

## Team-wide rollout (repo-controlled helper script)

Most iOS teams want the override applied for every developer on the team
without each person memorising the exact command. Ship a small script in
the repo:

```sh
# scripts/enable-bitrise-build-cache.sh
#!/usr/bin/env bash
set -euo pipefail

if ! command -v bitrise-build-cache >/dev/null; then
  echo "Install the CLI first: see docs/install.md" >&2
  exit 1
fi

# Auth env vars must already be set by the developer's shell rc.
# Workspace credentials are personal — never commit them.

bitrise-build-cache activate xcode
bitrise-build-cache daemon install
bitrise-build-cache daemon up
bitrise-build-cache xcode-app enable
```

Document in your repo's `README.md` that running
`./scripts/enable-bitrise-build-cache.sh` after the first `git clone` is
all that's needed.

> **Auth credentials stay personal.** Each developer's PAT is tied to their
> Bitrise account; never commit it to the repo. The helper script assumes
> the env vars are already exported by the developer's shell rc.

---

## Troubleshooting

| Symptom                                                            | First thing to try                                                                                  |
| ------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------- |
| `enable` errors with `xcelerate config not found`                  | Run `bitrise-build-cache activate xcode` first — that writes the proxy socket path enable depends on. |
| `enable` succeeds but builds aren't using the cache                | Relaunched Xcode? `launchctl setenv` only reaches processes launched after `enable` ran.            |
| `launchctl getenv XCODE_XCCONFIG_FILE` prints nothing              | The LaunchAgent failed to bootstrap. Re-run `enable` and read the error.                            |
| `bitrise-build-cache doctor` says the proxy is down                | `bitrise-build-cache daemon up`. Override expects the proxy listening.                              |
| Your project's existing `XCODE_XCCONFIG_FILE` was overwritten      | `disable` restores it. If state got out of sync, manually `launchctl setenv` to the prior path.     |
| Want to skip the cache for a single build                          | Hold ⌥ when clicking Run / pass `XCODE_XCCONFIG_FILE=""` to a one-off `xcodebuild` invocation.      |
| Multiple Xcode installs (Xcode-beta etc.)                          | The override applies to every Xcode launched after `setenv`. Only the GA `Xcode` process gets the relaunch nudge — beta users should quit-and-relaunch manually. |

For deeper logging, prepend `-d`
(`bitrise-build-cache -d xcode-app enable`). Open issues at
[github.com/bitrise-io/bitrise-build-cache-cli](https://github.com/bitrise-io/bitrise-build-cache-cli/issues).
