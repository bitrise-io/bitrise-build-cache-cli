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

Two ways to make a committed line safe:

### Option 1: Install the CLI on every machine

Simplest. On GitHub Actions add a step before `bazel`:

```yaml
- name: Install bitrise-build-cache
  run: |
    curl -sfL https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh \
      | sh -s -- -b /usr/local/bin
```

Same idea for other CI providers — install the CLI into a dir already on
`$PATH`. On developer laptops, `brew install bitrise-io/bitrise-build-cache/bitrise-build-cache`
or the same installer script.

`bitrise-build-cache activate bazel` and `bitrise-build-cache doctor` will
warn you when they detect a repo-level `.bazelrc` committing this pin — the
message points here.

### Option 2: Commit a self-installing shim (portable)

The CLI ships two helpers so a repo can commit the line without every consumer
having to install the CLI first. Run them once from the workspace root:

```sh
bitrise-build-cache bazel install-credhelper-shim
bitrise-build-cache bazel print-credhelper-line >> .bazelrc
```

- `install-credhelper-shim` writes `tools/bitrise-build-cache-credhelper.sh` —
  a small POSIX script that locates `bitrise-build-cache` on `$PATH`, and if
  it's missing, downloads and installs it into a workspace-local
  `.bitrise-cache/bin/` (idempotent — cached after the first call).
- `print-credhelper-line` emits:
  ```
  build --credential_helper=*.services.bitrise.io=%workspace%/tools/bitrise-build-cache-credhelper.sh
  ```
  which Bazel resolves against the workspace root at build time.

Commit both the shim and the `.bazelrc` line. Add `.bitrise-cache/` to
`.gitignore` — the shim populates it lazily.

The shim pins the CLI version that produced it. Regenerate with
`install-credhelper-shim --cli-version <tag>` to change the pin, or omit the
flag to track the running CLI's version.

Both subcommands accept `--dir <path>` if you prefer a location other than
`tools/`.

**Recommended companion setting.** Bazel keeps credential-helper responses in
its per-invocation cache; the duration is configurable via
`--experimental_credential_helper_cache_duration`. A generous value (e.g. `30m`)
avoids paying the shim's fork+exec cost on every remote call:

```
build --experimental_credential_helper_cache_duration=30m
```

**Residual tradeoffs even with the shim.** The shim mitigates the
"CLI not installed" failure mode, but a repo that commits it takes on two
things worth naming:

- *Cold-cache install fan-out.* Bazel launches credential-helper processes in
  parallel (`--loading_phase_threads`). The shim serialises the installer with
  a `flock`/spinlock gate and atomically moves the binary into place, so racers
  fall through to exec the winner's binary — but the winning process still
  pays a one-time download + unpack cost on the first build. The
  `--experimental_credential_helper_cache_duration` setting above bounds how
  often the shim is invoked at all.
- *A moving installer.* The shim's tag pin controls the CLI it downloads, and
  the `install/installer.sh` URL is pinned to the same tag as the CLI, so a
  committed shim is reproducible. Re-run `install-credhelper-shim` when you
  bump the pin.
