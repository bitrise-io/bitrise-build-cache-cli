# Install the Bitrise Build Cache CLI (local dev)

A one-page guide for getting the CLI running on a developer machine. For
CI / Bitrise stack provisioning the CLI is already installed and updated by
the platform — you don't need anything below.

This page covers the two supported install paths, the GAR fallback the
installer uses when github.com is unreachable, and the post-install steps to
get going.

---

## TL;DR

```sh
brew install bitrise-io/bitrise-build-cache/bitrise-build-cache
bitrise-build-cache --version
```

Or, without Homebrew:

```sh
curl --retry 5 -sSfL \
  'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' \
  | sh -s -- -b ~/.local/bin
bitrise-build-cache --version
```

Then set credentials + run an activate command — see [post-install](#post-install).

---

## Option 1 — Homebrew (macOS + Linuxbrew)

The Bitrise-maintained tap (`bitrise-io/homebrew-bitrise-build-cache`) ships
a formula that follows every CLI release within ~minutes of publication.

```sh
brew install bitrise-io/bitrise-build-cache/bitrise-build-cache
```

Upgrade later with:

```sh
brew update && brew upgrade bitrise-io/bitrise-build-cache/bitrise-build-cache
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

- Repo: `build-cache-cli-releases`
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

The CLI needs two pieces of information to talk to the Bitrise Build Cache
backend. Set them as environment variables in your shell rc file (`.zshrc`,
`.bashrc`, etc.):

```sh
export BITRISE_BUILD_CACHE_AUTH_TOKEN="<your bitrise PAT>"
export BITRISE_BUILD_CACHE_WORKSPACE_ID="<your workspace slug>"
```

Where:

1. **Bitrise authentication token** — either a
   [Personal Access Token (PAT)](https://devcenter.bitrise.io/en/accounts/personal-access-tokens.html)
   generated at [bitrise.io](https://bitrise.io) → user settings → Security
   → Personal access tokens, or a
   [Workspace API Token (WAT)](https://docs.bitrise.io/en/bitrise-platform/workspaces/workspace-api-token)
   if you'd rather use a workspace-scoped token than a personal one.
2. **Workspace ID** — the slug in the URL when you're on your Bitrise
   workspace page. See the
   [DevCenter slug guide](https://devcenter.bitrise.io/en/api/identifying-workspaces-and-apps-with-their-slugs.html).

> **Shortcut:** the easiest way to get both values is
> [bitrise.io/build-cache](https://app.bitrise.io/build-cache) → **Add new
> connection** — that page generates and displays both for you.

> **Bitrise CI:** the build VMs already expose these via
> [`BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN`](https://docs.bitrise.io/en/bitrise-build-cache/build-cache-for-gradle/configuring-the-build-cache-for-gradle-in-the-bitrise-ci-environment)
> so the CLI picks them up without further config. Set the two env vars above
> only when you're activating the cache outside Bitrise CI (local dev or a
> third-party CI provider).

Then configure the build tool(s) you use:

```sh
bitrise-build-cache activate gradle  --cache
bitrise-build-cache activate bazel   --cache
bitrise-build-cache activate xcode   --cache
bitrise-build-cache activate 'c++'
```

Pass `--help` to any of those for the full flag list.

---

## Verifying the install

```sh
bitrise-build-cache --version
bitrise-build-cache status
```

`status` reports which build tools (gradle / bazel / xcode / c++ /
react-native) are activated — run it after each `activate <tool>` to confirm
the activation took.

Run a build with `-d` (debug logging) the first time to confirm the cache
is being hit — for Gradle that's `gradle build -d`, for Bazel
`bazel build //... --verbose_failures`.

---

## Long-lived helper services (optional)

By default each `bitrise-build-cache activate` brings the helper processes
up in foreground / per-build mode. For local development where you build
repeatedly, registering the helpers as OS-supervised services keeps them
warm across shells:

```sh
bitrise-build-cache daemon install    # macOS LaunchAgent / Linux user systemd unit
bitrise-build-cache daemon up         # start the registered services
bitrise-build-cache daemon down       # stop without uninstalling
bitrise-build-cache daemon restart    # after a CLI upgrade
bitrise-build-cache daemon uninstall  # tear down
```

The daemon services are user-scoped (no root / sudo) and log to
`~/.local/state/bitrise-build-cache/logs/` (macOS) or via `journalctl --user`
(Linux).

---

## Troubleshooting

| Symptom                                  | First thing to try                                       |
| ---------------------------------------- | -------------------------------------------------------- |
| `command not found: bitrise-build-cache` | Confirm the bindir is on your `$PATH`. |
| Stale config after a CLI upgrade         | Re-run the relevant `bitrise-build-cache activate <tool>` (the CLI nudges you with the exact command on the next run after a version bump). |
| Auth errors / 401 from the cache backend | Re-check `BITRISE_BUILD_CACHE_AUTH_TOKEN` is exported and the PAT hasn't expired. |
| Network errors / GitHub unreachable      | The GAR fallback should kick in automatically; if not, see the URLs `installer.sh` printed. |
| Daemon services not running              | `bitrise-build-cache daemon up` (or `daemon install` if you never registered). |

For deeper logging on any command, prepend `-d`
(`bitrise-build-cache -d activate gradle ...`). If that doesn't surface
the root cause, open an issue on
[github.com/bitrise-io/bitrise-build-cache-cli](https://github.com/bitrise-io/bitrise-build-cache-cli/issues).
