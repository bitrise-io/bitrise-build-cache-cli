# Bazel + Bitrise Build Cache

The recommended flow is `bitrise-build-cache activate bazel`: it writes a
per-user `~/.bazelrc` block that points `--credential_helper` at the CLI on
disk. Nothing gets committed into the repo, so the setup can vary machine to
machine without breaking anyone else.

## Committing a credential-helper line

Sometimes a team wants the cache config to live in the repo — reviewed in PRs,
shared across every machine — instead of relying on each contributor to run
`activate`. The natural attempt is to move the generated line into
`<repo>/.bazelrc`:

```
build --credential_helper=*.services.bitrise.io=bitrise-build-cache
```

That works on any machine where `bitrise-build-cache` is on `$PATH`. When it
isn't (a fresh CI runner, a colleague on a new laptop), every Bazel command
fails hard with:

```
ERROR: Could not find file with name 'bitrise-build-cache' on PATH '...'
ERROR: Error initializing RemoteModule
```

The fix is to install the CLI on every machine that runs Bazel against this
repo, into a directory that is on `$PATH` **and is not treated as transient**.

### Install on GitHub Actions

```yaml
- name: Install Bitrise Build Cache CLI
  run: |
    mkdir -p "$HOME/.local/bin"
    curl -sfL https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh \
      | sh -s -- -b "$HOME/.local/bin"
    echo "$HOME/.local/bin" >> "$GITHUB_PATH"

- name: Activate Bitrise Build Cache for Bazel
  run: bitrise-build-cache activate bazel --cache --cache-push

- name: Bazel build
  run: bazel build //...
```

Why `~/.local/bin` and not `/tmp/bin`? The devcenter's older Bazel install
snippet uses `/tmp/bin`, which breaks the setup in two ways:

- `/tmp` is not on `$PATH` on a hosted GitHub Actions runner. `bazel` still
  looks the helper up by name and comes back with "not found".
- Even after you export the full path, `bitrise-build-cache activate bazel`
  detects that the running binary is under a transient prefix (`/tmp/`,
  `/var/folders/`, `/private/var/folders/`, `/private/tmp/`) and refuses to
  write a credential-helper reference that would break the next time the OS
  garbage-collected the directory. The generated `~/.bazelrc` silently falls
  back to a static Bearer header, which doesn't cooperate with a committed
  `--credential_helper` line.

`~/.local/bin` avoids both traps: it survives the runner's lifetime, it's a
one-line `$GITHUB_PATH` push away from being on `$PATH`, and it works on
hosted runners, self-hosted runners, and sudoless containers alike. If you
prefer another location, any persistent, on-PATH directory works —
`/usr/local/bin` on hosted runners (needs passwordless sudo), `$HOME/bin`,
`$RUNNER_TOOL_CACHE/...`, etc.

### Install on developer laptops

Same idea: install into a persistent, on-PATH directory. Either
`brew install bitrise-io/bitrise-build-cache/bitrise-build-cache` or the
`installer.sh` one-liner pointed at `~/.local/bin` will do.

`bitrise-build-cache activate bazel` also self-installs a copy of the running
binary into `~/.local/bin/bitrise-build-cache` when the CLI is not already on
`$PATH`, so a developer who ran the CLI once from `/tmp` or a downloaded
tarball ends up with a persistent copy without a second step. Add
`~/.local/bin` to `$PATH` to make bare-name resolution work.

`bitrise-build-cache activate bazel` and `bitrise-build-cache doctor` will
warn you when they detect the committed pin AND `bitrise-build-cache` is
missing from `$PATH` — the message points here. When the CLI IS on `$PATH`
the pin is silent-OK.
