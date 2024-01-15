# bitrise-build-cache-cli

Bitrise Build Cache CLI - to enable/configure Gradle or Bazel build cache on the machine where you run this CLI.


## Install

```shell
# download the Bitrise Build Cache CLI
curl -sSfL 'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' | sh -s -- -b /tmp/bin -d

# run the CLI
/tmp/bin/bitrise-build-cache-cli [COMMAND]
```

If you want to install the CLI to somewhere else you can change the `-b PATH` parameter.

### Command examples

To configure Bitrise Build Cache for Gradle on the current machine:

```shell
/tmp/bin/bitrise-build-cache-cli enable-for gradle
```

To configure Bitrise Build Cache for Bazel on the current machine:

```shell
/tmp/bin/bitrise-build-cache-cli enable-for bazel
```

For the options and parameters accepted by the commands call the command with `--help` flag.

The CLI requires the following environment variables to be set for authentication:

- If you're running it on Bitrise CI: no environment variable is required. Bitrise CI generates the necessary authentication config and exposes it as environment variable automatically for builds running on Bitrise CI.
- In any other CI environment:
  - Set `BITRISE_BUILD_CACHE_AUTH_TOKEN` to a Bitrise Personal Access Token which you can generate on [bitrise.io](https://bitrise.io/). Related documentation: [Bitrise DevCenter](https://devcenter.bitrise.io/en/accounts/personal-access-tokens.html#creating-a-personal-access-token).
  - Set the `BITRISE_BUILD_CACHE_WORKSPACE_ID` to the Bitrise Workspace's ID you have Bitrise Build Cache (Trial) enabled for. To find the Workspace ID navigate to the Workspace's page and find the ID in the URL. You can find the related documentation on the [Bitrise DevCenter](https://devcenter.bitrise.io/en/api/identifying-workspaces-and-apps-with-their-slugs.html#finding-a-slug-on-the-bitrise-website).

Note: the easiest way to get these parameters and do a Bitrise Build Cache setup is by going to [bitrise.io/build-cache](https://app.bitrise.io/build-cache), clicking `Add new connection` on the page and follow the guide there. It'll automatically generate and show the information you need for the setup.


## What does the CLI do on a high level?

It creates the necessary config to enable Build Cache and Command Exec/Invocation Analytics. It does this via adding the config in the `$HOME` directory.

In case of Gradle it's done via creating or modifying the following two files: `$HOME/.gradle/init.d/bitrise-build-cache-init.gradle` and `$HOME/.gradle/gradle.properties` (adding `org.gradle.caching=true` to `gradle.properties`).

In case of Bazel it's done via creating or modifying `$HOME/.bazelrc`.


## High level description of the process

When `enable-for gradle` or `enable-for bazel` is called:

1. CLI checks whether all the available inputs are available. Inputs (auth token, workspace ID, ...) are read from environment variables or via flags specified for the command.
2. Then it checks whether the configuration file(s) already exist in the `$HOME` directory.
4. Then it generates the build cache configuration content (merging with the current content of the configuration file(s) if the file(s) already exist).
5. And then it writes the configuration content into the config file(s).


## Technical notes

### Gradle

- `$HOME/.gradle/init.d/bitrise-build-cache-init.gradle` is overwritten when you run `enable-for gradle`.
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

## Release and download script generation

To release a new version we use [goreleaser](https://github.com/goreleaser/goreleaser).

**NOTE:** the release will be created as a **draft**. After successful release creation
and assets uploads (`goreleaser release --clean`) you have to manually finish the release
by editing the draft and clicking `Publish release`.

To generate the downloader/installer script we use [github.com/kamilsk/godownloader](https://github.com/kamilsk/godownloader).
