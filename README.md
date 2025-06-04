# Bitrise Build Cache CLI

Bitrise Build Cache CLI - to enable/configure Gradle or Bazel build cache on the machine where you run this CLI.


## Install

```shell
#!/usr/bin/env bash
set -euxo pipefail

# download the Bitrise Build Cache CLI
curl --retry 5 -sSfL 'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' | sh -s -- -b /tmp/bin -d

# run the CLI
/tmp/bin/bitrise-build-cache [COMMAND]
```

If you want to install the CLI to somewhere else you can change the `-b PATH` parameter.

If you want to install a specific version of the CLI you can use specify the version as the last parameter
of the installer script. For example to install version `v0.4.0`:

```shell
curl --retry 5 -sSfL 'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' | sh -s -- -b /tmp/bin -d v0.4.0
```

*Note: **DRAFT** versions aren't supported by the installer, but releases marked as **pre-release** are.*

### Command examples

To configure Bitrise Build Cache for Gradle on the current machine:

```shell
/tmp/bin/bitrise-build-cache enable-for gradle
```

To configure Bitrise Build Cache for Bazel on the current machine:

```shell
/tmp/bin/bitrise-build-cache enable-for bazel
```

For the options and parameters accepted by the commands call the command with `--help` flag.

The CLI requires the following environment variables to be set for authentication:

- If you're running it on Bitrise CI: no environment variable is required. Bitrise CI generates the necessary authentication config and exposes it as environment variable automatically for builds running on Bitrise CI.
- In any other CI environment:
  - Set `BITRISE_BUILD_CACHE_AUTH_TOKEN` to a Bitrise Personal Access Token which you can generate on [bitrise.io](https://bitrise.io/). Related documentation: [Bitrise DevCenter](https://devcenter.bitrise.io/en/accounts/personal-access-tokens.html#creating-a-personal-access-token).
  - Set the `BITRISE_BUILD_CACHE_WORKSPACE_ID` to the Bitrise Workspace's ID you have Bitrise Build Cache (Trial) enabled for. To find the Workspace ID navigate to the Workspace's page and find the ID in the URL. You can find the related documentation on the [Bitrise DevCenter](https://devcenter.bitrise.io/en/api/identifying-workspaces-and-apps-with-their-slugs.html#finding-a-slug-on-the-bitrise-website).

Note: the easiest way to get these parameters and do a Bitrise Build Cache setup is by going to [bitrise.io/build-cache](https://app.bitrise.io/build-cache), clicking `Add new connection` on the page and follow the guide there. It'll automatically generate and show the information you need for the setup.

Important: the `bitrise-build-cache` CLI configures the environment it's running in. If you're running commands in Docker containers you have to run the CLI in the same container in which you run Gradle/Bazel commands in.


## What does the CLI do on a high level?

It creates the necessary config to enable Build Cache and Command Exec/Invocation Analytics. It does this via adding the config in the `$HOME` directory.

In case of Gradle it's done via creating or modifying the following two files: `$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts` and `$HOME/.gradle/gradle.properties` (adding `org.gradle.caching=true` to `gradle.properties`).

In case of Bazel it's done via creating or modifying `$HOME/.bazelrc`.


## High level description of the process

When `enable-for gradle` or `enable-for bazel` is called:

1. CLI checks whether all the available inputs are available. Inputs (auth token, workspace ID, ...) are read from environment variables or via flags specified for the command.
2. Then it checks whether the configuration file(s) already exist in the `$HOME` directory.
4. Then it generates the build cache configuration content (merging with the current content of the configuration file(s) if the file(s) already exist).
5. And then it writes the configuration content into the config file(s).


## Technical notes

### Gradle

- `$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts` is overwritten when you run `enable-for gradle`.
  Any modification you do in that file will be overwritten.
- `$HOME/.gradle/gradle.properties` is modified in the following way: when you run `enable-for gradle`
  the CLI will check whether a `# [start] generated-by-bitrise-build-cache / # [end] generated-by-bitrise-build-cache`
  block is already in the file. If there is, then only the block's content will be modified.
  If there's no marked block in the properties file yet then the CLI will append it to the file
  with the necessary content in the block (`org.gradle.caching=true`).

### Bazel

- `$HOME/.bazelrc` is modified in the following way: when you run `enable-for bazel`
  the CLI will check whether a `# [start] generated-by-bitrise-build-cache / # [end] generated-by-bitrise-build-cache`
  block is already in the file. If there is, then only the block's content will be modified.
  If there's no marked block in the bazelrc file yet then the CLI will append it to the file
  with the necessary content in the block.

## Release process

### 1: Update Gradle verification reference file

Run the `bitrise test` or `bitrise run generate_gradle_verification` command locally, and commit changes (workflows will fail if there are any) to the repository. In case the CI workflow is still failing, copy the generated metadata from the `Generate Gradle verification reference` Step's log.

### 2: Create release

To release a new version we use [goreleaser](https://github.com/goreleaser/goreleaser).

The release is generated on Bitrise CI. To trigger it, create and push a new version tag
in the following format: `vX.X.X` for example `v0.1.0`:

```shell
# first tag the new release
git tag -a v0.1.0 -m "Initial test release"
git push origin v0.1.0
```

This will trigger a build on Bitrise CI with the `release` workflow,
which will create the new GitHub Release, generate the various archives
based on the `.goreleaser.yaml` config (currently generating intel + arm, for linux + macos).

**NOTE:** the GitHub Release will be created as a **draft**. After successful release creation
and assets uploads you have to manually finish the release by editing the draft and clicking `Publish release`
on the GitHub UI.

**NOTE:** to test the new release before it'd be automatically downloaded by the installer when no version
number is specified: edit the **draft** and enable the `Set as a pre-release` toggle option.
This way you can test the new version by specifying it for the installer script, and if it
looks good you can edit the release and change it to `Set as the latest release`,
so it'll be the version downloaded by the installer when the installer is called without a specified version.


### 3: Update downloader

Now that the new release is available run `godownloader` to update the
installer script.
To generate the downloader/installer script we use [github.com/kamilsk/godownloader](https://github.com/kamilsk/godownloader).
To install `godownloader` with Homebrew, use `brew install octolab/tap/godownloader`.

```shell
# generate the downloader/installer script into ./install/installer.sh based on the .goreleaser.yaml configuration.
godownloader .goreleaser.yaml > ./install/installer.sh
```

If only the timestamp is changed at the top of `./install/installer.sh` you don't have to commit
the change, just discard it. If something else also changes then commit and push the updated `installer.sh`.

### 4: Update depending repos (steps, ...)

The Bitrise Build Cache CLI is platform independent, but we have platform specific "adapters".
These adapters depend on the Bitrise Build Cache CLI, either by importing it as a Go
library, or by depending on the compiled and released CLI.

When we do a new Bitrise Build Cache CLI release the following dependent "adapters" should also be updated:

- Bitrise CI Steps:
  - https://github.com/bitrise-steplib/bitrise-step-activate-gradle-remote-cache
  - https://github.com/bitrise-steplib/bitrise-step-activate-bazel-cache
