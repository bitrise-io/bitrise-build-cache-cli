# Gradle Build Cache Step Releases

This page lists the releases of the [Bitrise Build Cache for
Gradle](https://devcenter.bitrise.io/en/dependencies-and-caching/remote-build-caching/remote-build-cache-for-gradle.html)
step and relevant dependencies. To start using it, please refer to the
linked DevCenter
[page](https://devcenter.bitrise.io/en/dependencies-and-caching/remote-build-caching/remote-build-cache-for-gradle.html#configuring-the-bitrise-build-cache-for-gradle-in-the-bitrise-ci-environment).

## Components

The relationship between the step and the dependencies is as follows:

- The Bitrise Build Cache for Gradle step
  (`activate-build-cache-for-gradle`, [repository
  link](https://github.com/bitrise-steplib/bitrise-step-activate-gradle-remote-cache))
  installs a specific version of the CLI.
- The Bitrise Build Cache CLI ([repository
  link](https://github.com/bitrise-io/bitrise-build-cache-cli)) supports
  multiple commands, but the relevant one is `enable-for gradle`, which
  creates the Gradle init script in
  `$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts` that pulls
  in the remote cache and analytics plugins with specific versions for
  the build.
- The analytics plugin `io.bitrise.gradle:gradle-analytics` is published
  to Maven Central and provides build analytics information (e.g.,
  [critical
  path](https://bitrise.io/changelog/enhanced-gradle-critical-path/24815)).
- The remote cache plugin `io.bitrise.gradle:remote-cache` is published
  to Maven Central and implements the client for the Bitrise remote
  Build Cache.

If you want to pin the dependencies (Gradle verification metadata), you
should pin the full (patch) step version in your Bitrise workflows.
Then, select the corresponding CLI version and use it to generate the
init script before writing the dependency verification metadata. This
ensures that the same dependencies are used. Additionally, you can find
the verification metadata generated in the CLI releases from v0.15.2
onwards.

## Releases

| Step version | CLI version | Analytics plugin version | Cache plugin version | Test Distribution plugin version |
|----|----|----|----|----|
| 2.7.54 | [v0.17.10](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.10) | 2.1.32 | 1.2.21 | 2.1.25 |
| 2.7.53 | [v0.17.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.9) | 2.1.32 | 1.2.21 | 2.1.25 |
| 2.7.52 | [v0.17.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.7) | 2.1.32 | 1.2.20 | 2.1.25 |
| 2.7.50 | [v0.17.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.6) | 2.1.32 | 1.2.20 | 2.1.25 |
| 2.7.49 | [v0.17.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.4) | 2.1.31 | 1.2.20 | 2.1.25 |
| 2.7.48 | [v0.17.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.3) | 2.1.30 | 1.2.20 | 2.1.25 |
| 2.7.47 | [v0.17.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.2) | 2.1.30 | 1.2.20 | 2.1.25 |
| 2.7.46 | [v0.17.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.1) | 2.1.30 | 1.2.20 | 2.1.25 |
| 2.7.44 | [v0.16.12](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.12) | 2.1.30 | 1.2.20 | 2.1.25 |
| 2.7.43 | [v0.16.11](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.11) | 2.1.28 | 1.2.19 | 2.1.24 |
| 2.7.42 | [v0.16.10](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.10) | 2.1.28 | 1.2.19 | 2.1.24 |
| 2.7.41 | [v0.16.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.9) | 2.1.28 | 1.2.19 |  |
| 2.7.40 | [v0.16.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.8) | 2.1.28 | 1.2.19 |  |
| 2.7.39 | [v0.16.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.7) | 2.1.28 | 1.2.19 |  |
| 2.7.38 | [v0.16.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.6) | 2.1.28 | 1.2.19 |  |
| 2.7.37 | [v0.16.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.2) | 2.1.28 | 1.2.19 |  |
| 2.7.36 | [v0.16.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.0) | 2.1.28 | 1.2.19 |  |
| 2.7.35 | [v0.15.38](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.38) | 2.1.28 | 1.2.19 |  |
| 2.7.34 | [v0.15.37](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.37) | 2.1.25 | 1.2.18 |  |
| 2.7.33 | [v0.15.36](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.36) | 2.1.25 | 1.2.18 |  |
| 2.7.32 | [v0.15.35](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.35) | 2.1.24 | 1.2.18 |  |
| 2.7.31 | [v0.15.34](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.34) | 2.1.24 | 1.2.17 |  |
| 2.7.30 | [v0.15.30](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.30) | 2.1.22 | 1.2.17 |  |
| 2.7.29 | [v0.15.29](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.29) | 2.1.21 | 1.2.17 |  |
| 2.7.28 | [v0.15.26](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.26) | 2.1.20 | 1.2.17 |  |
| 2.7.27 | [v0.15.25](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.25) | 2.1.19 | 1.2.17 |  |
| 2.7.26 | [v0.15.24](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.24) | 2.1.18 | 1.2.16 |  |
| 2.7.25 | [v0.15.22](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.22) | 2.1.17 | 1.2.16 |  |
| 2.7.24 | [v0.15.21](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.21) | 2.1.16 | 1.2.16 |  |
| 2.7.23 | [v0.15.20](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.20) | 2.1.15 | 1.2.16 |  |
| 2.7.22 | [v0.15.17](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.17) | 2.1.15 | 1.2.16 |  |
| 2.7.21 | [v0.15.16](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.16) | 2.1.14 | 1.2.16 |  |
| 2.7.20 | [v0.15.15](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.15) | 2.1.14 | 1.2.15 |  |
| 2.7.19 | [v0.15.14](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.14) | 2.1.13 | 1.2.15 |  |
| 2.7.18 | [v0.15.13](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.13) | 2.1.13 | 1.2.14 |  |
| 2.7.17 | [v0.15.12](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.12) | 2.1.13 | 1.2.14 |  |
| 2.7.16 | [v0.15.10](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.10) | 2.1.13 | 1.2.13 |  |
| 2.7.15 | [v0.15.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.9) | 2.1.12 | 1.2.12 |  |
| 2.7.14 | [v0.15.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.8) | 2.1.11 | 1.2.11 |  |
| 2.7.13 | [v0.15.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.7) | 2.1.11 | 1.2.10 |  |
| 2.7.12 | [v0.15.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.4) | 2.1.10 | 1.2.9 |  |
| 2.7.11 | [v0.15.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.2) | 2.1.8 | 1.2.9 |  |
| 2.7.10 | [v0.15.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.3) | 2.1.9 | 1.2.9 |  |
| 2.7.9 | [v0.15.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.2) | 2.1.8 | 1.2.9 |  |
| 2.7.8 | [v0.15.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.2) | 2.1.8 | 1.2.9 |  |
| 2.7.7 | [v0.14.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.8) | 2.1.7 | 1.2.9 |  |
| 2.7.6 | [v0.14.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.7) | 2.1.7 | 1.2.8 |  |
| 2.7.5 | [v0.14.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.5) | 2.1.7 | 1.2.6 |  |
| 2.7.4 | [v0.14.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.4) | 2.1.7 | 1.2.6 |  |
| 2.7.3 | [v0.14.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.3) | 2.1.6 | 1.2.6 |  |
| 2.7.2 | [v0.14.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.2) | 2.1.5 | 1.2.5 |  |
| 2.7.1 | [v0.14.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.1) | 2.1.4 | 1.2.5 |  |
| 2.7.0 | [v0.14.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.0) | 2.1.3 | 1.2.4 |  |
| 2.6.0 | [v0.13.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.13.0) | 2.1.3 | 1.2.4 |  |
| 2.5.0 | [v0.12.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.12.0) | 2.1.3 | 1.2.4 |  |
| 2.4.0 | [v0.11.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.11.0) | 2.1.3 | 1.2.4 |  |
| 2.3.0 | [v0.11.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.11.0) | 2.1.3 | 1.2.4 |  |
| 2.2.0 | [v0.10.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.10.0) | 2.1.3 | 1.2.4 |  |
| 2.1.0 | [v0.9.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.9.0) | 2.1.3 | 1.2.4 |  |
| 2.0.3 | [v0.8.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.3) | 2.1.3 | 1.2.4 |  |
| 2.0.2 | [v0.8.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.2) | 2.1.3 | 1.2.4 |  |
| 2.0.1 | [v0.8.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.0) | 2.1.3 | 1.2.4 |  |
| 2.0.0 | [v0.8.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.0) | 2.1.3 | 1.2.4 |  |
