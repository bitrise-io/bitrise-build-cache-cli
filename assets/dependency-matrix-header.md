# Bitrise Build Cache CLI — Dependency Matrix

This page lists every released version of the [Bitrise Build Cache CLI](https://github.com/bitrise-io/bitrise-build-cache-cli) and the corresponding plugin versions it pins for Gradle builds (Analytics, Remote Cache, Test Distribution, Common).

## Where the CLI runs

The CLI is downloaded and invoked by the Bitrise steps below. Pinning any of these steps to a specific patch version effectively pins the CLI version (and therefore the plugin versions) used in your build:

- [Activate Bitrise Build Cache for Gradle](https://github.com/bitrise-steplib/bitrise-step-activate-gradle-remote-cache) — `activate-build-cache-for-gradle`
- [Bitrise Build Cache for React Native](https://github.com/bitrise-steplib/bitrise-step-activate-react-native-features) — `activate-build-cache-for-react-native`
- [Activate Bitrise-hosted Gradle repository mirrors](https://github.com/bitrise-steplib/bitrise-step-activate-gradle-mirrors) — `activate-gradle-mirrors`

Different consumer steps may bundle different CLI versions, so the plugin versions in your build depend on which step actually runs first in your workflow. To find the CLI version a build used, look at the activating step's log — it prints the CLI version near the top of its section. Then look up that row below.

## How to read this page

Two views over the same data:

- **CLI release table** — start here if you know the CLI version a build used and want to know which plugin versions it pulled.
- **One section per consumer step** — start here if you know the step version pinned in your workflow and want the CLI + plugin versions it resolves to.

## Contents

- [CLI release table](#cli-release-table)
- [activate-build-cache-for-gradle](#activate-build-cache-for-gradle)
- [activate-build-cache-for-react-native](#activate-build-cache-for-react-native)
- [activate-gradle-mirrors](#activate-gradle-mirrors)

## Components

- The Bitrise Build Cache CLI ([repository link](https://github.com/bitrise-io/bitrise-build-cache-cli)) supports multiple commands; the relevant one here is `activate gradle` (or `enable-for gradle` in older versions), which writes the Gradle init script `$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts` that pulls in the remote cache, analytics, and test-distribution plugins with the versions listed below.
- The analytics plugin `io.bitrise.gradle:gradle-analytics` is published to Maven Central and provides build analytics (e.g. [critical path](https://bitrise.io/changelog/enhanced-gradle-critical-path/24815)).
- The remote cache plugin `io.bitrise.gradle:remote-cache` is published to Maven Central and implements the client for the Bitrise remote build cache.
- The test distribution plugin `io.bitrise.gradle:test-distribution` is published to Maven Central and integrates with Bitrise Test Distribution.
- The common plugin `io.bitrise.gradle:common` is published to Maven Central and is a shared dependency of the above.

## Gradle dependency verification

If you use Gradle's [dependency verification](https://docs.gradle.org/current/userguide/dependency_verification.html), pin the activating step to a full patch version in your workflow and seed your `gradle/verification-metadata.xml` from the matching CLI release. Each CLI release page attaches a reference asset:

- `verification-metadata.xml` — checksums for artifacts as served by the original Maven Central / Google / plugin-portal repositories. Use this when the Bitrise Maven Central mirror is **not** active in your workflow.
- `verification-metadata-mirror.xml` (from `v2.4.6` onward) — checksums for artifacts as served by the **Bitrise Maven Central mirror**. The mirror is on by default for all Bitrise Android builds, so this is the file most workflows need; the plain origin file will fail verification when the mirror serves the artifacts.

For the full setup walkthrough — including which step in your workflow activates the mirror, how to lock that step's version, and how to regenerate the metadata when the CLI is updated — see [Gradle dependency verification for the Maven Central mirror](https://docs.google.com/document/d/1mrquZ-n7dNNmQo0o4ddzY73JTsY5xkYRgRKARoKNFvs/edit).
