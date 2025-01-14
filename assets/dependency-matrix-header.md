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
