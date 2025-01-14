# Gradle Build Cache Step Releases

This page lists the releases of the [Bitrise Build Cache for Gradle](https://devcenter.bitrise.io/en/dependencies-and-caching/remote-build-caching/remote-build-cache-for-gradle.html) step and relevant dependencies. To start using it, please refer to the linked DevCenter [page](https://devcenter.bitrise.io/en/dependencies-and-caching/remote-build-caching/remote-build-cache-for-gradle.html#configuring-the-bitrise-build-cache-for-gradle-in-the-bitrise-ci-environment).

## Components

The relationship between the step and the dependencies is as follows:
- The Bitrise Build Cache for Gradle step (`activate-build-cache-for-gradle`, [repository link](https://github.com/bitrise-steplib/bitrise-step-activate-gradle-remote-cache)) installs a specific version of the CLI.
- The Bitrise Build Cache CLI ([repository link](https://github.com/bitrise-io/bitrise-build-cache-cli)) supports multiple commands, but the relevant one is `enable-for gradle`, which creates the Gradle init script in `$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts` that pulls in the remote cache and analytics plugins with specific versions for the build.
- The analytics plugin `io.bitrise.gradle:gradle-analytics` is published to Maven Central and provides build analytics information (e.g., [critical path](https://bitrise.io/changelog/enhanced-gradle-critical-path/24815)).
- The remote cache plugin `io.bitrise.gradle:remote-cache` is published to Maven Central and implements the client for the Bitrise remote Build Cache.

If you want to pin the dependencies (Gradle verification metadata), you should pin the full (patch) step version in your Bitrise workflows. Then, select the corresponding CLI version and use it to generate the init script before writing the dependency verification metadata. This ensures that the same dependencies are used. Additionally, you can find the verification metadata generated in the CLI releases from v0.15.2 onwards.

## Releases
| Step version | CLI version | Analytics plugin version | Cache plugin version |
|--------------|----------------------------------------------------------------------------------------|--------------------------|----------------------|
| 2.7.13       | [v0.15.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.15.7) | 2.1.11                   | 1.2.10               |
| 2.7.12       | [v0.15.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.15.4) | 2.1.10                   | 1.2.9               |
| 2.7.11       | [v0.15.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.15.2) | 2.1.8                   | 1.2.9               |
| 2.7.10       | [v0.15.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.15.3) | 2.1.9                   | 1.2.9               |
| 2.7.9       | [v0.15.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.15.2) | 2.1.8                   | 1.2.9               |
| 2.7.8       | [v0.15.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.15.2) | 2.1.8                   | 1.2.9               |
| 2.7.7       | [v0.14.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.14.8) | 2.1.7                   | 1.2.9               |
| 2.7.6       | [v0.14.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.14.7) | 2.1.7                   | 1.2.8               |
| 2.7.5       | [v0.14.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.14.5) | 2.1.7                   | 1.2.6               |
| 2.7.4       | [v0.14.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.14.4) | 2.1.7                   | 1.2.6               |
| 2.7.3       | [v0.14.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.14.3) | 2.1.6                   | 1.2.6               |
| 2.7.2       | [v0.14.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.14.2) | 2.1.5                   | 1.2.5               |
| 2.7.1       | [v0.14.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.14.1) | 2.1.4                   | 1.2.5               |
| 2.7.0       | [v0.14.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.14.0) | 2.1.3                   | 1.2.4               |
| 2.6.0       | [v0.13.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.13.0) | 2.1.3                   | 1.2.4               |
| 2.5.0       | [v0.12.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.12.0) | 2.1.3                   | 1.2.4               |
| 2.4.0       | [v0.11.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.11.0) | 2.1.3                   | 1.2.4               |
| 2.3.0       | [v0.11.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.11.0) | 2.1.3                   | 1.2.4               |
| 2.2.0       | [v0.10.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.10.0) | 2.1.3                   | 1.2.4               |
| 2.1.0       | [v0.9.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.9.0) | 2.1.3                   | 1.2.4               |
| 2.0.3       | [v0.8.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.8.3) | 2.1.3                   | 1.2.4               |
| 2.0.2       | [v0.8.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.8.2) | 2.1.3                   | 1.2.4               |
| 2.0.1       | [v0.8.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.8.0) | 2.1.3                   | 1.2.4               |
| 2.0.0       | [v0.8.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag//v0.8.0) | 2.1.3                   | 1.2.4               |
