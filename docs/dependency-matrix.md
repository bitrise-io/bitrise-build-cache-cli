# Bitrise Build Cache CLI — Dependency Matrix

This page lists every released version of the [Bitrise Build Cache
CLI](https://github.com/bitrise-io/bitrise-build-cache-cli) and the
corresponding plugin versions it pins for Gradle builds (Analytics,
Remote Cache, Test Distribution, Common).

## Where the CLI runs

The CLI is downloaded and invoked by the Bitrise steps below. Pinning
any of these steps to a specific patch version effectively pins the CLI
version (and therefore the plugin versions) used in your build:

- [Activate Bitrise Build Cache for
  Gradle](https://github.com/bitrise-steplib/bitrise-step-activate-gradle-remote-cache)
  — `activate-build-cache-for-gradle`
- [Bitrise Build Cache for React
  Native](https://github.com/bitrise-steplib/bitrise-step-activate-react-native-features)
  — `activate-build-cache-for-react-native`
- [Activate Bitrise-hosted Gradle repository
  mirrors](https://github.com/bitrise-steplib/bitrise-step-activate-gradle-mirrors)
  — `activate-gradle-mirrors`

Different consumer steps may bundle different CLI versions, so the
plugin versions in your build depend on which step actually runs first
in your workflow. To find the CLI version a build used, look at the
activating step’s log — it prints the CLI version near the top of its
section. Then look up that row below.

## How to read this page

Two views over the same data:

- **CLI release table** — start here if you know the CLI version a build
  used and want to know which plugin versions it pulled.
- **One section per consumer step** — start here if you know the step
  version pinned in your workflow and want the CLI + plugin versions it
  resolves to.

## Contents

- [CLI release table](#cli-release-table)
- [activate-build-cache-for-gradle](#activate-build-cache-for-gradle)
- [activate-build-cache-for-react-native](#activate-build-cache-for-react-native)
- [activate-gradle-mirrors](#activate-gradle-mirrors)

## Components

- The Bitrise Build Cache CLI ([repository
  link](https://github.com/bitrise-io/bitrise-build-cache-cli)) supports
  multiple commands; the relevant one here is `activate gradle` (or
  `enable-for gradle` in older versions), which writes the Gradle init
  script `$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts` that
  pulls in the remote cache, analytics, and test-distribution plugins
  with the versions listed below.
- The analytics plugin `io.bitrise.gradle:gradle-analytics` is published
  to Maven Central and provides build analytics (e.g. [critical
  path](https://bitrise.io/changelog/enhanced-gradle-critical-path/24815)).
- The remote cache plugin `io.bitrise.gradle:remote-cache` is published
  to Maven Central and implements the client for the Bitrise remote
  build cache.
- The test distribution plugin `io.bitrise.gradle:test-distribution` is
  published to Maven Central and integrates with Bitrise Test
  Distribution.
- The common plugin `io.bitrise.gradle:common` is published to Maven
  Central and is a shared dependency of the above.

## Gradle dependency verification

If you use Gradle’s [dependency
verification](https://docs.gradle.org/current/userguide/dependency_verification.html),
pin the activating step to a full patch version in your workflow and
seed your `gradle/verification-metadata.xml` from the matching CLI
release. Each CLI release page attaches a reference asset:

- `verification-metadata.xml` — checksums for artifacts as served by the
  original Maven Central / Google / plugin-portal repositories. Use this
  when the Bitrise Maven Central mirror is **not** active in your
  workflow.
- `verification-metadata-mirror.xml` (from `v2.4.6` onward) — checksums
  for artifacts as served by the **Bitrise Maven Central mirror**. The
  mirror is on by default for all Bitrise Android builds, so this is the
  file most workflows need; the plain origin file will fail verification
  when the mirror serves the artifacts.

For the full setup walkthrough — including which step in your workflow
activates the mirror, how to lock that step’s version, and how to
regenerate the metadata when the CLI is updated — see [Gradle dependency
verification for the Maven Central
mirror](https://docs.google.com/document/d/1mrquZ-n7dNNmQo0o4ddzY73JTsY5xkYRgRKARoKNFvs/edit).

## CLI release table

| CLI version | Release date | Analytics plugin | Cache plugin | Test Distribution plugin | Common plugin |
|----|----|----|----|----|----|
| [v2.8.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.6) | 2026-06-25 | 2.7.4 | 1.3.4 | 2.2.10 | 1.0.7 |
| [v2.8.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.5) | 2026-06-19 | 2.7.4 | 1.3.4 | 2.2.10 | 1.0.7 |
| [v2.8.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.4) | 2026-06-04 | 2.7.4 | 1.3.4 | 2.2.10 | 1.0.7 |
| [v2.8.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.3) | 2026-06-02 | 2.7.4 | 1.3.4 | 2.2.10 | 1.0.7 |
| [v2.8.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.2) | 2026-06-02 | 2.7.4 | 1.3.4 | 2.2.10 | 1.0.7 |
| [v2.8.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.1) | 2026-05-28 | 2.7.4 | 1.3.4 | 2.2.10 | 1.0.7 |
| [v2.8.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.0) | 2026-05-26 | 2.7.3 | 1.3.4 | 2.2.10 | 1.0.7 |
| [v2.7.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.5) | 2026-05-22 | 2.7.3 | 1.3.4 | 2.2.10 | 1.0.7 |
| [v2.7.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.4) | 2026-05-22 | 2.7.3 | 1.3.4 | 2.2.10 | 1.0.7 |
| [v2.7.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.3) | 2026-05-19 | 2.7.3 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.7.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.2) | 2026-05-18 | 2.7.3 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.7.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.1) | 2026-05-15 | 2.7.2 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.7.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.0) | 2026-05-15 | 2.7.2 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.6.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.7) | 2026-05-15 | 2.7.2 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.6.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.6) | 2026-05-13 | 2.7.2 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.6.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.5) | 2026-05-13 | 2.7.2 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.6.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.4) | 2026-05-13 | 2.7.2 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.6.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.3) | 2026-05-13 | 2.7.2 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.6.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.2) | 2026-05-13 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.6.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.1) | 2026-05-12 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.6.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.0) | 2026-05-05 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.5.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.5.0) | 2026-05-05 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.4.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.9) | 2026-04-30 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.4.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.8) | 2026-04-30 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.4.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.7) | 2026-04-30 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.4.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.6) | 2026-04-30 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.4.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.5) | 2026-04-30 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.4.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.4) | 2026-04-30 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.4.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.3) | 2026-04-30 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.4.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.2) | 2026-04-29 | 2.7.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.4.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.1) | 2026-04-29 | 2.7.0 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.4.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.0) | 2026-04-29 | 2.7.0 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.3.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.3.4) | 2026-04-28 | 2.7.0 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.3.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.3.3) | 2026-04-28 | 2.7.0 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.3.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.3.2) | 2026-04-28 | 2.7.0 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.3.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.3.1) | 2026-04-28 | 2.7.0 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.3.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.3.0) | 2026-04-27 | 2.7.0 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.2.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.2.2) | 2026-04-23 | 2.6.1 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.2.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.2.1) | 2026-04-22 | 2.6.0 | 1.3.3 | 2.2.10 | 1.0.7 |
| [v2.2.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.2.0) | 2026-04-21 | 2.6.0 | 1.3.2 | 2.2.10 | 1.0.7 |
| [v2.1.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.1.1) | 2026-04-21 | 2.6.0 | 1.3.1 | 2.2.10 | 1.0.7 |
| [v2.1.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.1.0) | 2026-04-21 | 2.6.0 | 1.3.1 | 2.2.10 | 1.0.7 |
| [v2.0.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.0.2) | 2026-04-20 | 2.6.0 | 1.3.1 | 2.2.10 | 1.0.7 |
| [v2.0.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.0.1) | 2026-04-14 | 2.6.0 | 1.3.1 | 2.2.10 | 1.0.7 |
| [v2.0.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.0.0) | 2026-04-14 | 2.6.0 | 1.3.1 | 2.2.10 | 1.0.7 |
| [v1.6.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.6.0) | 2026-04-10 | 2.6.0 | 1.3.1 | 2.2.10 | 1.0.7 |
| [v1.5.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.5.5) | 2026-04-02 | 2.6.0 | 1.3.1 | 2.2.10 | 1.0.7 |
| [v1.5.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.5.4) | 2026-04-01 | 2.6.0 | 1.3.0 | 2.2.10 | 1.0.7 |
| [v1.5.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.5.3) | 2026-03-31 | 2.6.0 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.5.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.5.2) | 2026-03-30 | 2.5.4 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.5.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.5.1) | 2026-03-30 | 2.5.4 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.5.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.5.0) | 2026-03-26 | 2.5.3 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.4.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.4.0) | 2026-03-20 | 2.5.3 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.3.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.3.5) | 2026-03-19 | 2.5.3 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.3.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.3.4) | 2026-03-18 | 2.5.3 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.3.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.3.3) | 2026-03-17 | 2.5.2 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.3.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.3.2) | 2026-03-13 | 2.5.1 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.3.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.3.1) | 2026-03-13 | 2.4.4 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.3.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.3.0) | 2026-03-11 | 2.4.3 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.2.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.2.0) | 2026-03-10 | 2.4.1 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.1.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.1.1) | 2026-03-03 | 2.4.1 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.1.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.1.0) | 2026-03-02 | 2.4.1 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.0.43](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.43) | 2026-02-24 | 2.4.0 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.0.42](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.42) | 2026-02-12 | 2.3.0 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.0.41](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.41) | 2026-02-03 | 2.2.5 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.0.40](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.40) | 2026-01-26 | 2.2.5 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.0.39](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.39) | 2026-01-22 | 2.2.5 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.0.38](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.38) | 2026-01-13 | 2.2.5 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.0.37](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.37) | 2026-01-12 | 2.2.4 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.0.36](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.36) | 2025-12-17 | 2.2.3 | 1.2.28 | 2.2.10 | 1.0.7 |
| [v1.0.35](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.35) | 2025-12-15 | 2.2.3 | 1.2.28 | 2.2.9 | 1.0.7 |
| [v1.0.34](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.34) | 2025-12-10 | 2.2.3 | 1.2.28 | 2.2.8 | 1.0.7 |
| [v1.0.33](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.33) | 2025-12-10 | 2.2.3 | 1.2.28 | 2.2.7 | 1.0.7 |
| [v1.0.32](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.32) | 2025-12-09 | 2.2.3 | 1.2.28 | 2.2.7 | 1.0.7 |
| [v1.0.31](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.31) | 2025-12-08 | 2.2.3 | 1.2.28 | 2.2.6 | 1.0.7 |
| [v1.0.30](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.30) | 2025-12-08 | 2.2.3 | 1.2.27 | 2.2.5 | 1.0.7 |
| [v1.0.29](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.29) | 2025-12-08 | 2.2.3 | 1.2.27 | 2.2.5 | 1.0.7 |
| [v1.0.28](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.28) | 2025-12-01 | 2.2.2 | 1.2.26 | 2.2.4 | 1.0.6 |
| [v1.0.27](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.27) | 2025-11-28 | 2.2.2 | 1.2.26 | 2.2.4 | 1.0.6 |
| [v1.0.26](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.26) | 2025-11-27 | 2.2.2 | 1.2.25 | 2.2.4 | 1.0.6 |
| [v1.0.25](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.25) | 2025-11-26 | 2.2.2 | 1.2.25 | 2.2.3 | 1.0.6 |
| [v1.0.24](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.24) | 2025-11-20 | 2.2.2 | 1.2.25 | 2.2.2 | 1.0.6 |
| [v1.0.23](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.23) | 2025-11-20 | 2.2.2 | 1.2.25 | 2.2.1 | 1.0.6 |
| [v1.0.22](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.22) | 2025-11-20 | 2.2.2 | 1.2.25 | 2.2.1 | 1.0.6 |
| [v1.0.21](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.21) | 2025-11-20 | 2.2.2 | 1.2.25 | 2.2.1 | 1.0.6 |
| [v1.0.20](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.20) | 2025-11-20 | 2.2.2 | 1.2.25 | 2.2.0 | 1.0.6 |
| [v1.0.19](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.19) | 2025-11-19 | 2.2.1 | 1.2.25 | 2.2.0 | 1.0.6 |
| [v1.0.18](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.18) | 2025-11-19 | 2.2.1 | 1.2.25 | 2.2.0 | 1.0.6 |
| [v1.0.17](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.17) | 2025-11-19 | 2.2.1 | 1.2.25 | 2.2.0 | 1.0.6 |
| [v1.0.16](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.16) | 2025-11-18 | 2.2.0 | 1.2.24 | 2.1.28 | 1.0.5 |
| [v1.0.15](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.15) | 2025-11-17 | 2.2.0 | 1.2.24 | 2.1.28 | 1.0.5 |
| [v1.0.14](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.14) | 2025-11-13 | 2.2.0 | 1.2.24 | 2.1.28 | 1.0.5 |
| [v1.0.13](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.13) | 2025-11-05 | 2.1.36 | 1.2.24 | 2.1.28 | 1.0.5 |
| [v1.0.12](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.12) | 2025-11-04 | 2.1.36 | 1.2.24 | 2.1.28 | 1.0.5 |
| [v1.0.11](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.11) | 2025-10-31 | 2.1.35 | 1.2.23 | 2.1.27 | 1.0.4 |
| [v1.0.10](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.10) | 2025-10-29 | 2.1.35 | 1.2.23 | 2.1.27 | 1.0.4 |
| [v1.0.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.9) | 2025-10-28 | 2.1.35 | 1.2.23 | 2.1.27 | 1.0.4 |
| [v1.0.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.8) | 2025-10-28 | 2.1.35 | 1.2.23 | 2.1.27 | 1.0.4 |
| [v1.0.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.7) | 2025-10-28 | 2.1.35 | 1.2.23 | 2.1.27 | 1.0.4 |
| [v1.0.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.6) | 2025-10-28 | 2.1.35 | 1.2.23 | 2.1.27 | 1.0.4 |
| [v1.0.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.5) | 2025-10-13 | 2.1.33 | 1.2.21 | 2.1.25 | 1.0.2 |
| [v1.0.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.4) | 2025-10-01 | 2.1.33 | 1.2.21 | 2.1.25 | 1.0.2 |
| [v1.0.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.3) | 2025-09-29 | 2.1.32 | 1.2.21 | 2.1.25 | 1.0.2 |
| [v1.0.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.2) | 2025-09-25 | 2.1.32 | 1.2.21 | 2.1.25 | 1.0.2 |
| [v1.0.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.1) | 2025-09-23 | 2.1.32 | 1.2.21 | 2.1.25 | 1.0.2 |
| [v0.17.10](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.10) | 2025-09-04 | 2.1.32 | 1.2.21 | 2.1.25 | 1.0.2 |
| [v0.17.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.9) | 2025-09-02 | 2.1.32 | 1.2.21 | 2.1.25 | 1.0.2 |
| [v0.17.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.8) | 2025-08-07 | 2.1.32 | 1.2.20 | 2.1.25 | 1.0.2 |
| [v0.17.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.7) | 2025-07-30 | 2.1.32 | 1.2.20 | 2.1.25 | 1.0.2 |
| [v0.17.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.6) | 2025-07-29 | 2.1.32 | 1.2.20 | 2.1.25 | 1.0.2 |
| [v0.17.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.5) | 2025-07-10 | 2.1.31 | 1.2.20 | 2.1.25 | 1.0.2 |
| [v0.17.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.4) | 2025-07-08 | 2.1.31 | 1.2.20 | 2.1.25 | 1.0.2 |
| [v0.17.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.3) | 2025-07-08 | 2.1.30 | 1.2.20 | 2.1.25 | 1.0.2 |
| [v0.17.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.2) | 2025-07-01 | 2.1.30 | 1.2.20 | 2.1.25 | 1.0.2 |
| [v0.17.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.1) | 2025-07-01 | 2.1.30 | 1.2.20 | 2.1.25 | 1.0.2 |
| [v0.17.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.17.0) | 2025-06-27 | 2.1.30 | 1.2.20 | 2.1.25 | 1.0.2 |
| [v0.16.12](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.12) | 2025-06-26 | 2.1.30 | 1.2.20 | 2.1.25 | 1.0.2 |
| [v0.16.11](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.11) | 2025-06-25 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.16.10](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.10) | 2025-06-19 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.16.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.9) | 2025-06-17 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.16.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.8) | 2025-06-17 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.16.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.7) | 2025-06-11 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.16.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.6) | 2025-06-11 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.16.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.5) | 2025-06-11 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.16.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.4) | 2025-06-11 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.16.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.3) | 2025-06-11 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.16.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.2) | 2025-06-11 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.16.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.1) | 2025-06-11 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.16.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.0) | 2025-06-04 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.15.38](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.38) | 2025-06-03 | 2.1.28 | 1.2.19 | 2.1.24 | 1.0.1 |
| [v0.15.37](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.37) | 2025-05-13 | 2.1.25 | 1.2.18 | \- | \- |
| [v0.15.36](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.36) | 2025-05-09 | 2.1.25 | 1.2.18 | \- | \- |
| [v0.15.35](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.35) | 2025-05-08 | 2.1.24 | 1.2.18 | \- | \- |
| [v0.15.34](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.34) | 2025-04-30 | 2.1.24 | 1.2.17 | \- | \- |
| [v0.15.33](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.33) | 2025-04-30 | 2.1.23 | 1.2.17 | \- | \- |
| [v0.15.32](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.32) | 2025-04-24 | 2.1.22 | 1.2.17 | \- | \- |
| [v0.15.31](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.31) | 2025-04-15 | 2.1.22 | 1.2.17 | \- | \- |
| [v0.15.30](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.30) | 2025-04-10 | 2.1.22 | 1.2.17 | \- | \- |
| [v0.15.29](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.29) | 2025-04-09 | 2.1.21 | 1.2.17 | \- | \- |
| [v0.15.28](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.28) | 2025-04-07 | 2.1.20 | 1.2.17 | \- | \- |
| [v0.15.27](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.27) | 2025-04-07 | 2.1.20 | 1.2.17 | \- | \- |
| [v0.15.26](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.26) | 2025-04-04 | 2.1.20 | 1.2.17 | \- | \- |
| [v0.15.25](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.25) | 2025-04-04 | 2.1.19 | 1.2.17 | \- | \- |
| [v0.15.24](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.24) | 2025-04-01 | 2.1.18 | 1.2.16 | \- | \- |
| [v0.15.23](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.23) | 2025-03-31 | 2.1.17 | 1.2.16 | \- | \- |
| [v0.15.22](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.22) | 2025-03-26 | 2.1.17 | 1.2.16 | \- | \- |
| [v0.15.21](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.21) | 2025-03-25 | 2.1.16 | 1.2.16 | \- | \- |
| [v0.15.20](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.20) | 2025-03-18 | 2.1.15 | 1.2.16 | \- | \- |
| [v0.15.19](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.19) | 2025-03-18 | 2.1.15 | 1.2.16 | \- | \- |
| [v0.15.18](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.18) | 2025-03-18 | 2.1.15 | 1.2.16 | \- | \- |
| [v0.15.17](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.17) | 2025-03-04 | 2.1.15 | 1.2.16 | \- | \- |
| [v0.15.16](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.16) | 2025-02-06 | 2.1.14 | 1.2.16 | \- | \- |
| [v0.15.15](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.15) | 2025-01-31 | 2.1.14 | 1.2.15 | \- | \- |
| [v0.15.14](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.14) | 2025-01-30 | 2.1.13 | 1.2.15 | \- | \- |
| [v0.15.13](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.13) | 2025-01-28 | 2.1.13 | 1.2.14 | \- | \- |
| [v0.15.12](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.12) | 2025-01-27 | 2.1.13 | 1.2.14 | \- | \- |
| [v0.15.11](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.11) | 2025-01-24 | 2.1.13 | 1.2.13 | \- | \- |
| [v0.15.10](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.10) | 2025-01-21 | 2.1.13 | 1.2.13 | \- | \- |
| [v0.15.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.9) | 2025-01-17 | 2.1.12 | 1.2.12 | \- | \- |
| [v0.15.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.8) | 2025-01-16 | 2.1.11 | 1.2.11 | \- | \- |
| [v0.15.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.7) | 2025-01-13 | 2.1.11 | 1.2.10 | \- | \- |
| [v0.15.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.6) | 2025-01-13 | 2.1.11 | 1.2.9 | \- | \- |
| [v0.15.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.5) | 2025-01-09 | 2.1.10 | 1.2.9 | \- | \- |
| [v0.15.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.4) | 2024-12-12 | 2.1.10 | 1.2.9 | \- | \- |
| [v0.15.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.3) | 2024-12-09 | 2.1.9 | 1.2.9 | \- | \- |
| [v0.15.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.2) | 2024-12-02 | 2.1.8 | 1.2.9 | \- | \- |
| [v0.15.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.1) | 2024-11-25 | 2.1.7 | 1.2.9 | \- | \- |
| [v0.15.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.0) | 2024-10-16 | 2.1.7 | 1.2.9 | \- | \- |
| [v0.14.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.8) | 2024-09-30 | 2.1.7 | 1.2.9 | \- | \- |
| [v0.14.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.7) | 2024-08-02 | 2.1.7 | 1.2.8 | \- | \- |
| [v0.14.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.6) | 2024-07-22 | 2.1.7 | 1.2.7 | \- | \- |
| [v0.14.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.5) | 2024-07-03 | 2.1.7 | 1.2.6 | \- | \- |
| [v0.14.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.4) | 2024-07-01 | 2.1.7 | 1.2.6 | \- | \- |
| [v0.14.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.3) | 2024-05-29 | 2.1.6 | 1.2.6 | \- | \- |
| [v0.14.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.2) | 2024-05-17 | 2.1.5 | 1.2.5 | \- | \- |
| [v0.14.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.1) | 2024-05-15 | 2.1.4 | 1.2.5 | \- | \- |
| [v0.14.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.0) | 2024-05-13 | 2.1.3 | 1.2.4 | \- | \- |
| [v0.13.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.13.0) | 2024-05-08 | 2.1.3 | 1.2.4 | \- | \- |
| [v0.12.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.12.0) | 2024-04-23 | 2.1.2 | 1.2.3 | \- | \- |
| [v0.11.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.11.0) | 2024-04-03 | \- | 1.2.3 | \- | \- |
| [v0.10.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.10.0) | 2024-03-26 | \- | \- | \- | \- |
| [v0.9.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.9.0) | 2024-02-27 | \- | \- | \- | \- |
| [v0.8.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.3) | 2024-02-20 | 2.0.2 | \- | \- | \- |
| [v0.8.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.2) | 2024-02-16 | 2.0.1 | \- | \- | \- |
| [v0.8.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.1) | 2024-02-02 | \- | \- | \- | \- |
| [v0.8.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.0) | 2024-01-30 | \- | \- | \- | \- |
| [v0.7.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.7.0) | 2024-01-26 | \- | \- | \- | \- |
| [v0.6.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.6.0) | 2024-01-26 | \- | \- | \- | \- |
| [v0.5.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.5.0) | 2024-01-16 | \- | \- | \- | \- |
| [v0.4.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.4.0) | 2024-01-15 | \- | \- | \- | \- |
| [v0.3.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.3.0) | 2024-01-15 | \- | \- | \- | \- |
| [v0.2.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.2.0) | 2024-01-15 | \- | \- | \- | \- |
| [v0.1.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.1.0) | 2024-01-15 | \- | \- | \- | \- |

## activate-build-cache-for-gradle

| Step version | CLI version | Analytics plugin | Cache plugin | Test Distribution plugin |
|----|----|----|----|----|
| 2.20.11 | [v2.8.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.6) | 2.7.4 | 1.3.4 | 2.2.10 |
| 2.20.10 | [v2.8.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.5) | 2.7.4 | 1.3.4 | 2.2.10 |
| 2.20.9 | [v2.8.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.4) | 2.7.4 | 1.3.4 | 2.2.10 |
| 2.20.7 | [v2.7.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.5) | 2.7.3 | 1.3.4 | 2.2.10 |
| 2.20.6 | [v2.7.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.3) | 2.7.3 | 1.3.3 | 2.2.10 |
| 2.20.5 | [v2.7.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.2) | 2.7.3 | 1.3.3 | 2.2.10 |
| 2.20.4 | [v2.6.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.7) | 2.7.2 | 1.3.3 | 2.2.10 |
| 2.20.3 | [v2.6.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.3) | 2.7.2 | 1.3.3 | 2.2.10 |
| 2.20.2 | [v2.6.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.2) | 2.7.1 | 1.3.3 | 2.2.10 |
| 2.20.1 | [v2.6.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.0) | 2.7.1 | 1.3.3 | 2.2.10 |
| 2.20.0 | [v2.6.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.0) | 2.7.1 | 1.3.3 | 2.2.10 |
| 2.19.0 | [v2.5.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.5.0) | 2.7.1 | 1.3.3 | 2.2.10 |
| 2.18.9 | [v2.4.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.9) | 2.7.1 | 1.3.3 | 2.2.10 |
| 2.18.8 | [v2.4.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.8) | 2.7.1 | 1.3.3 | 2.2.10 |
| 2.18.7 | [v2.4.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.7) | 2.7.1 | 1.3.3 | 2.2.10 |
| 2.18.6 | [v2.4.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.6) | 2.7.1 | 1.3.3 | 2.2.10 |
| 2.18.5 | [v2.4.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.4) | 2.7.1 | 1.3.3 | 2.2.10 |
| 2.18.4 | [v2.4.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.3) | 2.7.1 | 1.3.3 | 2.2.10 |
| 2.18.3 | [v2.4.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.2) | 2.7.1 | 1.3.3 | 2.2.10 |
| 2.18.2 | [v2.4.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.1) | 2.7.0 | 1.3.3 | 2.2.10 |
| 2.18.1 | [v2.4.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.1) | 2.7.0 | 1.3.3 | 2.2.10 |
| 2.18.0 | [v2.4.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.0) | 2.7.0 | 1.3.3 | 2.2.10 |
| 2.17.2 | [v2.3.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.3.4) | 2.7.0 | 1.3.3 | 2.2.10 |
| 2.17.1 | [v2.3.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.3.3) | 2.7.0 | 1.3.3 | 2.2.10 |
| 2.17.0 | [v2.3.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.3.0) | 2.7.0 | 1.3.3 | 2.2.10 |
| 2.16.2 | [v2.2.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.2.2) | 2.6.1 | 1.3.3 | 2.2.10 |
| 2.16.1 | [v2.2.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.2.1) | 2.6.0 | 1.3.3 | 2.2.10 |
| 2.16.0 | [v2.2.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.2.0) | 2.6.0 | 1.3.2 | 2.2.10 |
| 2.15.0 | [v2.1.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.1.0) | 2.6.0 | 1.3.1 | 2.2.10 |
| 2.14.1 | [v1.6.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.6.0) | 2.6.0 | 1.3.1 | 2.2.10 |
| 2.14.0 | [v1.6.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.6.0) | 2.6.0 | 1.3.1 | 2.2.10 |
| 2.13.9 | [v1.5.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.5.5) | 2.6.0 | 1.3.1 | 2.2.10 |
| 2.13.8 | [v1.5.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.5.4) | 2.6.0 | 1.3.0 | 2.2.10 |
| 2.13.7 | [v1.5.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.5.3) | 2.6.0 | 1.2.28 | 2.2.10 |
| 2.13.6 | [v1.5.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.5.2) | 2.5.4 | 1.2.28 | 2.2.10 |
| 2.13.5 | [v1.5.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.5.1) | 2.5.4 | 1.2.28 | 2.2.10 |
| 2.13.4 | [v1.3.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.3.4) | 2.5.3 | 1.2.28 | 2.2.10 |
| 2.13.3 | [v1.3.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.3.3) | 2.5.2 | 1.2.28 | 2.2.10 |
| 2.13.2 | [v1.3.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.3.2) | 2.5.1 | 1.2.28 | 2.2.10 |
| 2.13.1 | [v1.3.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.3.1) | 2.4.4 | 1.2.28 | 2.2.10 |
| 2.13.0 | [v1.3.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.3.0) | 2.4.3 | 1.2.28 | 2.2.10 |
| 2.12.0 | [v1.1.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.1.1) | 2.4.1 | 1.2.28 | 2.2.10 |
| 2.11.0 | [v1.1.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.1.0) | 2.4.1 | 1.2.28 | 2.2.10 |
| 2.10.0 | [v1.0.43](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.43) | 2.4.0 | 1.2.28 | 2.2.10 |
| 2.9.0 | [v1.0.42](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.42) | 2.3.0 | 1.2.28 | 2.2.10 |
| 2.8.8 | [v1.0.38](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.38) | 2.2.5 | 1.2.28 | 2.2.10 |
| 2.8.7 | [v1.0.37](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.37) | 2.2.4 | 1.2.28 | 2.2.10 |
| 2.8.4 | [v1.0.27](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.27) | 2.2.2 | 1.2.26 | 2.2.4 |
| 2.8.3 | [v1.0.25](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.25) | 2.2.2 | 1.2.25 | 2.2.3 |
| 2.8.2 | [v1.0.20](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.20) | 2.2.2 | 1.2.25 | 2.2.0 |
| 2.8.1 | [v1.0.17](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.17) | 2.2.1 | 1.2.25 | 2.2.0 |
| 2.8.0 | [v1.0.14](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.14) | 2.2.0 | 1.2.24 | 2.1.28 |
| 2.7.60 | [v1.0.13](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.13) | 2.1.36 | 1.2.24 | 2.1.28 |
| 2.7.59 | [v1.0.12](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.12) | 2.1.36 | 1.2.24 | 2.1.28 |
| 2.7.58 | [v1.0.10](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.10) | 2.1.35 | 1.2.23 | 2.1.27 |
| 2.7.57 | [v1.0.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.9) | 2.1.35 | 1.2.23 | 2.1.27 |
| 2.7.56 | [v1.0.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.6) | 2.1.35 | 1.2.23 | 2.1.27 |
| 2.7.55 | [v1.0.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v1.0.4) | 2.1.33 | 1.2.21 | 2.1.25 |
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
| 2.7.41 | [v0.16.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.9) | 2.1.28 | 1.2.19 | 2.1.24 |
| 2.7.40 | [v0.16.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.8) | 2.1.28 | 1.2.19 | 2.1.24 |
| 2.7.39 | [v0.16.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.7) | 2.1.28 | 1.2.19 | 2.1.24 |
| 2.7.38 | [v0.16.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.6) | 2.1.28 | 1.2.19 | 2.1.24 |
| 2.7.37 | [v0.16.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.2) | 2.1.28 | 1.2.19 | 2.1.24 |
| 2.7.36 | [v0.16.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.16.0) | 2.1.28 | 1.2.19 | 2.1.24 |
| 2.7.35 | [v0.15.38](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.38) | 2.1.28 | 1.2.19 | 2.1.24 |
| 2.7.34 | [v0.15.37](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.37) | 2.1.25 | 1.2.18 | \- |
| 2.7.33 | [v0.15.36](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.36) | 2.1.25 | 1.2.18 | \- |
| 2.7.32 | [v0.15.35](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.35) | 2.1.24 | 1.2.18 | \- |
| 2.7.31 | [v0.15.34](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.34) | 2.1.24 | 1.2.17 | \- |
| 2.7.30 | [v0.15.30](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.30) | 2.1.22 | 1.2.17 | \- |
| 2.7.29 | [v0.15.29](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.29) | 2.1.21 | 1.2.17 | \- |
| 2.7.28 | [v0.15.26](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.26) | 2.1.20 | 1.2.17 | \- |
| 2.7.27 | [v0.15.25](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.25) | 2.1.19 | 1.2.17 | \- |
| 2.7.26 | [v0.15.24](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.24) | 2.1.18 | 1.2.16 | \- |
| 2.7.25 | [v0.15.22](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.22) | 2.1.17 | 1.2.16 | \- |
| 2.7.24 | [v0.15.21](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.21) | 2.1.16 | 1.2.16 | \- |
| 2.7.23 | [v0.15.20](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.20) | 2.1.15 | 1.2.16 | \- |
| 2.7.22 | [v0.15.17](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.17) | 2.1.15 | 1.2.16 | \- |
| 2.7.21 | [v0.15.16](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.16) | 2.1.14 | 1.2.16 | \- |
| 2.7.20 | [v0.15.15](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.15) | 2.1.14 | 1.2.15 | \- |
| 2.7.19 | [v0.15.14](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.14) | 2.1.13 | 1.2.15 | \- |
| 2.7.18 | [v0.15.13](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.13) | 2.1.13 | 1.2.14 | \- |
| 2.7.17 | [v0.15.12](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.12) | 2.1.13 | 1.2.14 | \- |
| 2.7.16 | [v0.15.10](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.10) | 2.1.13 | 1.2.13 | \- |
| 2.7.15 | [v0.15.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.9) | 2.1.12 | 1.2.12 | \- |
| 2.7.14 | [v0.15.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.8) | 2.1.11 | 1.2.11 | \- |
| 2.7.13 | [v0.15.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.7) | 2.1.11 | 1.2.10 | \- |
| 2.7.12 | [v0.15.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.4) | 2.1.10 | 1.2.9 | \- |
| 2.7.11 | [v0.15.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.2) | 2.1.8 | 1.2.9 | \- |
| 2.7.10 | [v0.15.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.3) | 2.1.9 | 1.2.9 | \- |
| 2.7.9 | [v0.15.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.2) | 2.1.8 | 1.2.9 | \- |
| 2.7.8 | [v0.15.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.15.2) | 2.1.8 | 1.2.9 | \- |
| 2.7.7 | [v0.14.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.8) | 2.1.7 | 1.2.9 | \- |
| 2.7.6 | [v0.14.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.7) | 2.1.7 | 1.2.8 | \- |
| 2.7.5 | [v0.14.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.5) | 2.1.7 | 1.2.6 | \- |
| 2.7.4 | [v0.14.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.4) | 2.1.7 | 1.2.6 | \- |
| 2.7.3 | [v0.14.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.3) | 2.1.6 | 1.2.6 | \- |
| 2.7.2 | [v0.14.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.2) | 2.1.5 | 1.2.5 | \- |
| 2.7.1 | [v0.14.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.1) | 2.1.4 | 1.2.5 | \- |
| 2.7.0 | [v0.14.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.14.0) | 2.1.3 | 1.2.4 | \- |
| 2.6.0 | [v0.13.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.13.0) | 2.1.3 | 1.2.4 | \- |
| 2.5.0 | [v0.12.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.12.0) | 2.1.2 | 1.2.3 | \- |
| 2.4.0 | [v0.11.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.11.0) | \- | 1.2.3 | \- |
| 2.3.0 | [v0.11.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.11.0) | \- | 1.2.3 | \- |
| 2.2.0 | [v0.10.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.10.0) | \- | \- | \- |
| 2.1.0 | [v0.9.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.9.0) | \- | \- | \- |
| 2.0.3 | [v0.8.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.3) | 2.0.2 | \- | \- |
| 2.0.2 | [v0.8.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.2) | 2.0.1 | \- | \- |
| 2.0.1 | [v0.8.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.0) | \- | \- | \- |
| 2.0.0 | [v0.8.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v0.8.0) | \- | \- | \- |

## activate-build-cache-for-react-native

| Step version | CLI version | Analytics plugin | Cache plugin | Test Distribution plugin |
|----|----|----|----|----|
| 0.8.8 | [v2.8.6](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.6) | 2.7.4 | 1.3.4 | 2.2.10 |
| 0.8.7 | [v2.8.5](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.5) | 2.7.4 | 1.3.4 | 2.2.10 |
| 0.8.6 | [v2.8.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.4) | 2.7.4 | 1.3.4 | 2.2.10 |
| 0.8.5 | [v2.8.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.3) | 2.7.4 | 1.3.4 | 2.2.10 |
| 0.8.4 | [v2.8.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.8.1) | 2.7.4 | 1.3.4 | 2.2.10 |
| 0.8.3 | [v2.7.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.3) | 2.7.3 | 1.3.3 | 2.2.10 |
| 0.8.2 | [v2.7.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.2) | 2.7.3 | 1.3.3 | 2.2.10 |
| 0.8.1 | [v2.7.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.1) | 2.7.2 | 1.3.3 | 2.2.10 |
| 0.8.0 | [v2.7.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.7.0) | 2.7.2 | 1.3.3 | 2.2.10 |
| 0.7.3 | [v2.6.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.3) | 2.7.2 | 1.3.3 | 2.2.10 |
| 0.7.2 | [v2.6.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.2) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.7.1 | [v2.6.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.0) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.7.0 | [v2.6.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.0) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.6.0 | [v2.5.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.5.0) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.5.7 | [v2.4.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.9) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.5.6 | [v2.4.8](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.8) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.5.5 | [v2.4.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.7) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.5.4 | [v2.4.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.4) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.5.3 | [v2.4.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.2) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.5.2 | [v2.4.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.1) | 2.7.0 | 1.3.3 | 2.2.10 |
| 0.5.1 | [v2.4.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.1) | 2.7.0 | 1.3.3 | 2.2.10 |
| 0.5.0 | [v2.4.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.0) | 2.7.0 | 1.3.3 | 2.2.10 |
| 0.4.1 | [v2.3.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.3.4) | 2.7.0 | 1.3.3 | 2.2.10 |
| 0.4.0 | [v2.3.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.3.0) | 2.7.0 | 1.3.3 | 2.2.10 |
| 0.3.0 | [v2.2.2](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.2.2) | 2.6.1 | 1.3.3 | 2.2.10 |
| 0.2.0 | [v2.0.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.0.1) | 2.6.0 | 1.3.1 | 2.2.10 |

## activate-gradle-mirrors

| Step version | CLI version | Analytics plugin | Cache plugin | Test Distribution plugin |
|----|----|----|----|----|
| 0.2.1 | [v2.6.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.6.1) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.2.0 | [v2.5.0](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.5.0) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.1.4 | [v2.4.9](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.9) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.1.3 | [v2.4.7](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.7) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.1.2 | [v2.4.4](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.4) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.1.1 | [v2.4.3](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.3) | 2.7.1 | 1.3.3 | 2.2.10 |
| 0.1.0 | [v2.4.1](https://github.com/bitrise-io/bitrise-build-cache-cli/releases/tag/v2.4.1) | 2.7.0 | 1.3.3 | 2.2.10 |
