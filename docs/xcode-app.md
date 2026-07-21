# Use the Bitrise Build Cache from Xcode.app (GUI builds)

A one-page guide for enabling the compile cache when you press ▶ in
Xcode.app. The default `xcodebuild` wrapper only kicks in for command-line
builds — this page covers the GUI case.

For the underlying CLI install + auth, see
[`docs/install.md`](install.md) first.

> **Xcode 26+ IDE remote-CAS limitation.** On macOS 26 / Xcode 26.x, IDE
> (⌘B) builds engage the **local** compile-cache plugin only — remote CAS
> (the `xcelerate-proxy` socket) is not dialed even after `xcode-app enable`
> and `xcode-app link`. Root cause: Xcode's build system recognises only
> `COMPILATION_CACHE_ENABLE_CACHING` / `_DEFAULT` /
> `_ENABLE_DIAGNOSTIC_REMARKS` as build settings, and silently drops
> `COMPILATION_CACHE_REMOTE_SERVICE_PATH` before writing `.cas-config`.
> Remote CAS still engages for `xcodebuild` CLI (with the wrapper installed
> via `activate xcode`). Full write-up:
> [`xcode-app-ide-remote-cas-findings-2026-07-21.md`](xcode-app-ide-remote-cas-findings-2026-07-21.md).

---

## TL;DR

```sh
# 1) one-time CLI install (see docs/install.md)
brew install bitrise-io/bitrise-build-cache/bitrise-build-cache

# 2) interactive sign-in — browser SSO, workspace picker, token stored
#    in the OS keychain (auto-refreshed on subsequent CLI calls)
bitrise-build-cache auth login

# 3) configure the Xcode compile cache (writes ~/.bitrise-xcelerate/config.json)
bitrise-build-cache activate xcode

# 4) enable the override for Xcode.app GUI builds
#    (also installs + starts the xcelerate-proxy daemon for you)
bitrise-build-cache xcode-app enable

# 5) relaunch Xcode, then build any target
```

Prefer env vars over the browser flow? Set `BITRISE_BUILD_CACHE_AUTH_TOKEN` + `BITRISE_BUILD_CACHE_WORKSPACE_ID` in your shell rc instead of step 2 — see [post-install](install.md#post-install). Env vars always take precedence over the keychain-stored token.

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

> **On Xcode 26+ IDE, the socket is never dialed.** The env propagates and
> settings resolve, but Xcode's build system drops
> `COMPILATION_CACHE_REMOTE_SERVICE_PATH` before writing the per-target
> `.cas-config` — so the compile-cache plugin has no remote endpoint.
> `xcode-app link` (below) engages the local plugin (real IDE speedup); the
> remote path today is `xcodebuild` CLI with the `activate xcode` wrapper.
> See
> [`xcode-app-ide-remote-cas-findings-2026-07-21.md`](xcode-app-ide-remote-cas-findings-2026-07-21.md)
> for the full test matrix and mitigation options.

---

## Prerequisites

| Step                                             | Why                                                                                                        |
| ------------------------------------------------ | ---------------------------------------------------------------------------------------------------------- |
| `bitrise-build-cache --version` works            | Confirms the CLI is on `$PATH`. See [`docs/install.md`](install.md).                                       |
| Authenticated                                    | Either run `bitrise-build-cache auth login` (browser SSO + OS-keychain-stored token, auto-refreshed) **or** export `BITRISE_BUILD_CACHE_AUTH_TOKEN` + `BITRISE_BUILD_CACHE_WORKSPACE_ID`. Env vars take precedence when both are set. See [post-install](install.md#post-install). |
| `bitrise-build-cache activate xcode` has run     | Writes `~/.bitrise-xcelerate/config.json` containing the proxy socket path that `xcode-app enable` reads.  |

`xcode-app enable` installs + starts the xcelerate-proxy service itself (filtered subset of `daemon install + up`), so you don't need to run those separately. If you've already registered the daemon for other tools (ccache etc.), the proxy install is a no-op. `xcode-app enable` errors with a clear hint if the CLI / auth / `activate xcode` prerequisite is missing.

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

If you already had `XCODE_XCCONFIG_FILE` pointing at a project xcconfig, you'll see an extra line above the LaunchAgent confirmation:

```
Chained previous XCODE_XCCONFIG_FILE: /Users/<you>/path/to/your/Base.xcconfig
```

See [Chaining an existing `XCODE_XCCONFIG_FILE`](#chaining-an-existing-xcode_xcconfig_file) for what that means.

Re-running `enable` after a successful `enable` is safe — the activator detects the self-loop (`launchctl getenv XCODE_XCCONFIG_FILE` now points at our own override) and preserves the original prior-path state on disk, so `disable` later still restores the right xcconfig.

> **Relaunch Xcode after enable.** `launchctl setenv` only reaches processes
> launched *after* it ran. Already-open Xcode windows pick up the override
> only on the next launch. Use ⌘Q (not just close the window).

---

## Verify the override is in effect

After relaunching Xcode, three quick checks:

```sh
# 1. The env is set in the current GUI session:
launchctl getenv XCODE_XCCONFIG_FILE
#  → should print ~/.bitrise-xcelerate/xcode-app.xcconfig

# 2. The setenv LaunchAgent is loaded (re-applies the env on every login):
launchctl list | grep io.bitrise.build-cache
#  → should list io.bitrise.build-cache.xcode-app-setenv (and the proxy)

# 3. The proxy socket exists and is listening:
socket_path=$(jq -r .proxySocketPath ~/.bitrise-xcelerate/config.json)
test -S "$socket_path" && echo "proxy socket present at $socket_path"

# 4. Build any target in Xcode. New entries should appear under
#    ~/.local/state/xcelerate/logs/ — that's the proxy log directory.
ls -lt ~/.local/state/xcelerate/logs/ | head
```

If `launchctl getenv` prints nothing, either the initial `launchctl setenv`
or the LaunchAgent bootstrap failed during `enable` — re-run
`bitrise-build-cache xcode-app enable` and read the error in its output.

If the socket isn't there, the xcelerate-proxy service didn't start. Re-run
`enable` (which calls daemon install + up internally) or, equivalently,
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

# Trigger interactive sign-in when no creds are resolvable (env vars → keychain → analytics config).
# `auth token` exits non-zero when nothing resolves; `auth login` is a no-op if the keychain already has a valid token.
if ! bitrise-build-cache auth token >/dev/null 2>&1; then
  bitrise-build-cache auth login
fi

bitrise-build-cache activate xcode
bitrise-build-cache daemon install
bitrise-build-cache daemon up
bitrise-build-cache xcode-app enable
```

Document in your repo's `README.md` that running
`./scripts/enable-bitrise-build-cache.sh` after the first `git clone` is
all that's needed.

> **Auth credentials stay personal.** Each developer either runs
> `bitrise-build-cache auth login` once (browser SSO, token in OS
> keychain, auto-refreshed) or exports their own PAT via
> `BITRISE_BUILD_CACHE_AUTH_TOKEN` — never commit either to the repo.

---

## Pre-build self-check (catch a broken setup at ⌘B)

The override survives logout, but the daemon can be killed, the PAT can
rotate, and `activate xcode` can be overwritten by a teammate. To catch
these before `swiftc` starts and burns minutes producing a no-cache build,
wire `bitrise-build-cache doctor` into the scheme's **Build → Pre-actions**
as a Run Script action. Full recipe (paste-in script + hard-block variant
+ team rollout via shared `.xcscheme`) in
[`xcode-scheme-self-check.md`](xcode-scheme-self-check.md).

---

## Troubleshooting

| Symptom                                                            | First thing to try                                                                                  |
| ------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------- |
| `enable` errors with `xcelerate config not found`                  | Run `bitrise-build-cache activate xcode` first — that writes the proxy socket path enable depends on. |
| `enable` succeeds but builds aren't using the cache                | Relaunched Xcode? `launchctl setenv` only reaches processes launched after `enable` ran. **On Xcode 26+ IDE, remote CAS won't engage regardless — see the limitation banner at the top of this page.** |
| `launchctl getenv XCODE_XCCONFIG_FILE` prints nothing              | Either the initial `launchctl setenv` failed or the LaunchAgent bootstrap did. Re-run `enable` and read the error in its output. |
| Proxy socket missing (`test -S` fails on the path in `config.json`) | Re-run `bitrise-build-cache xcode-app enable` (it ensures the daemon service is installed + up), or `bitrise-build-cache daemon up`. Override expects the proxy listening. |
| Your project's existing `XCODE_XCCONFIG_FILE` was overwritten      | `disable` restores it. If state got out of sync, manually `launchctl setenv` to the prior path.     |
| Want to skip the cache for a single CLI build                      | Pass `XCODE_XCCONFIG_FILE=""` to a one-off `xcodebuild` invocation (CLI args win over the override slot). For GUI builds, edit the scheme → Build → Pre-actions and add `unset XCODE_XCCONFIG_FILE` in a "New Run Script Action" — ⌥-clicking Run only opens the scheme editor, it does not bypass the env. |
| Multiple Xcode installs (Xcode-beta etc.)                          | The override applies to every Xcode launched after `setenv`. Only the GA `Xcode` process gets the relaunch nudge — beta users should quit-and-relaunch manually. |

For deeper logging, prepend `-d`
(`bitrise-build-cache -d xcode-app enable`). Open issues at
[github.com/bitrise-io/bitrise-build-cache-cli](https://github.com/bitrise-io/bitrise-build-cache-cli/issues).
