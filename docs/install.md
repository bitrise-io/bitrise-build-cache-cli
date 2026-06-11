# Install the Bitrise Build Cache CLI (local dev)

A one-page guide for getting the CLI running on a developer machine. For
CI / Bitrise stack provisioning the CLI is already installed and updated by
the platform — you don't need anything below.

This page covers the two supported install paths plus the GAR fallback the
installer uses when github.com is unreachable, and the post-install steps to
get going.

---

## TL;DR

```sh
brew install bitrise-io/tools/bitrise-build-cache-cli
bitrise-build-cache --version
```

Or, without Homebrew:

```sh
curl --retry 5 -sSfL \
  'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' \
  | sh -s -- -b ~/.local/bin
bitrise-build-cache --version
```

Then run `bitrise-build-cache activate --interactive` (see [post-install](#post-install)).

---

## Option 1 — Homebrew (macOS + Linuxbrew)

The Bitrise-maintained tap (`bitrise-io/tools`) ships a formula that follows
every CLI release within ~minutes of publication.

```sh
brew install bitrise-io/tools/bitrise-build-cache-cli
```

Upgrade later with:

```sh
brew update && brew upgrade bitrise-io/tools/bitrise-build-cache-cli
```

The CLI's `bitrise-build-cache update` subcommand detects a Homebrew install
and prints the exact upgrade command — no need to memorise it.

**Release cadence + notes** — releases land roughly weekly. The tap formula
auto-bumps from the [GitHub releases page](https://github.com/bitrise-io/bitrise-build-cache-cli/releases),
where every tag carries the changelog and the attached platform tarballs.

---

## Option 2 — `installer.sh` (manual / scriptable)

The pipe-curl-to-sh form is the install path the Bitrise platform itself
uses inside every build. It works anywhere there's `curl` + `sh`, including
headless servers and CI runners that aren't Bitrise.

```sh
curl --retry 5 -sSfL \
  'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' \
  | sh -s -- -b ~/.local/bin
```

Pin a specific version by appending the tag:

```sh
curl --retry 5 -sSfL \
  'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' \
  | sh -s -- -b ~/.local/bin v0.17.0
```

Notes:

- `-b <bindir>` is where the binary lands. Pick a directory on your `$PATH`
  (`~/.local/bin`, `/usr/local/bin`, `~/bin`, etc.).
- Pre-release versions install fine if you pass an explicit tag. Without a
  tag, the installer picks the latest non-pre-release.
- The script verifies a sha256 checksum against the release's `checksums`
  file before writing the binary — a corrupted download is a hard failure,
  not a silently broken install.

Upgrade later with `bitrise-build-cache update` (re-runs the installer
against the same bindir).

### GAR fallback

If `github.com` or `raw.githubusercontent.com` is unreachable, `installer.sh`
falls back to a Google Artifact Registry mirror that holds the same tarballs
+ checksums file + a copy of `installer.sh` itself:

- Bucket: `build-cache-cli-releases`
- Project: `ip-build-cache-prod`
- Region: `us-central1`
- Public read.

The fallback is automatic — there's no flag to flip. The first thing
`installer.sh` does on a failed github.com lookup is try the GAR URL for
the latest tag (`installer.sh:latest-pointer:VERSION`) and the GAR tarball
URLs. If both paths fail, the install errors out with a clear message
showing which URLs were attempted.

---

## Post-install

The CLI is configured per-shell with two pieces of information:

1. **Bitrise Personal Access Token** (PAT) — generated at
   [bitrise.io](https://bitrise.io) → user settings → Security → Personal
   access tokens. See the
   [DevCenter guide](https://devcenter.bitrise.io/en/accounts/personal-access-tokens.html).
2. **Workspace ID** — the slug in the URL when you're on your Bitrise
   workspace page.

Run the interactive wizard to wire those up plus pick which build tool(s)
to configure (Gradle / Bazel / Xcode / ccache):

```sh
bitrise-build-cache activate --interactive
```

The wizard masks the PAT entry and writes the resolved credentials to the
OS keychain so you don't have to leave them in your shell rc files. (See
the keychain section of [`activate --interactive`](#) for opt-out / migration
notes.)

If you'd rather configure a single tool directly:

```sh
bitrise-build-cache activate gradle  --cache
bitrise-build-cache activate bazel   --cache
bitrise-build-cache activate xcode   --cache
bitrise-build-cache activate c++
```

---

## Verifying the install

```sh
bitrise-build-cache --version
bitrise-build-cache doctor          # full health check (auth, proxy, ccache, log dirs)
bitrise-build-cache status          # one-shot "is it working right now?"
```

`doctor` includes a `--fix` flag for the repairs it knows are safe — handy
after a partial install or a stale config.

---

## Long-lived helpers (optional)

By default each `bitrise-build-cache activate` brings the helper processes
up in foreground / per-build mode. For local development where you build
repeatedly, registering the helpers as OS-supervised services keeps them
warm across shells:

```sh
bitrise-build-cache daemon install   # macOS LaunchAgent / Linux user systemd unit
bitrise-build-cache daemon status    # check
bitrise-build-cache daemon restart   # after a CLI upgrade
bitrise-build-cache daemon uninstall # tear down
```

The daemon services are user-scoped (no root / sudo) and log to
`~/.local/state/bitrise-build-cache/logs/` (macOS) or via `journalctl --user`
(Linux).

---

## Troubleshooting

| Symptom                                       | First thing to try                                       |
| --------------------------------------------- | -------------------------------------------------------- |
| `command not found: bitrise-build-cache`      | Confirm `~/.local/bin` (or wherever you installed) is on `$PATH`. |
| Stale config after a CLI upgrade              | `bitrise-build-cache activate <tool>` (or follow the auto-nudge after the next run). |
| Auth errors / 401 from the cache backend      | `bitrise-build-cache auth set` to re-enter the PAT, then `doctor`. |
| Network errors / GitHub unreachable           | The GAR fallback should kick in automatically; if not, see the URLs `installer.sh` printed. |
| Daemon services not running                   | `bitrise-build-cache daemon status` → `daemon up` if registered, `daemon install` if not. |

If `doctor` doesn't surface the root cause, capture the run with
`bitrise-build-cache -d doctor --json` and open an issue on
[github.com/bitrise-io/bitrise-build-cache-cli](https://github.com/bitrise-io/bitrise-build-cache-cli/issues).
