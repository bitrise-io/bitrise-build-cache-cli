# Bitrise Build Cache CLI — Dependency Matrix

This page lists every released version of the [Bitrise Build Cache CLI](https://github.com/bitrise-io/bitrise-build-cache-cli) and the corresponding plugin versions it pins for Gradle builds (Analytics, Remote Cache, Test Distribution).

## Where the CLI runs

The CLI is downloaded and invoked by several Bitrise steps. Pinning any of these steps to a specific patch version effectively pins the CLI version (and therefore the plugin versions) used in your build:

- [Activate Gradle Remote Cache](https://github.com/bitrise-steplib/bitrise-step-activate-gradle-remote-cache) — `activate-build-cache-for-gradle`
- [Activate React Native Features](https://github.com/bitrise-steplib/bitrise-step-activate-react-native-features) — `activate-react-native-features` (RN cache)
- [Activate Gradle Features](https://github.com/bitrise-steplib/bitrise-step-activate-gradle-features) — `activate-gradle-features` (experimental)
- [Install missing Android SDK components](https://github.com/bitrise-steplib/steps-install-missing-android-tools) — `install-missing-android-tools`
- [Activate Gradle Mirrors](https://github.com/bitrise-steplib/bitrise-step-activate-gradle-mirrors) — `activate-gradle-mirrors`
- [Gradle Runner](https://github.com/bitrise-steplib/steps-gradle-runner) — `gradle-runner`

Different consumer steps may bundle different CLI versions, so the plugin versions in your build depend on which step actually runs first in your workflow. To find the CLI version a build used, look at the activating step's log — it prints the CLI version near the top of its section. Then look up that row below.

## Components

- The Bitrise Build Cache CLI ([repository link](https://github.com/bitrise-io/bitrise-build-cache-cli)) supports multiple commands; the relevant one here is `activate gradle` (or `enable-for gradle` in older versions), which writes the Gradle init script `$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts` that pulls in the remote cache, analytics, and test-distribution plugins with the versions listed below.
- The analytics plugin `io.bitrise.gradle:gradle-analytics` is published to Maven Central and provides build analytics (e.g. [critical path](https://bitrise.io/changelog/enhanced-gradle-critical-path/24815)).
- The remote cache plugin `io.bitrise.gradle:remote-cache` is published to Maven Central and implements the client for the Bitrise remote build cache.
- The test distribution plugin `io.bitrise.gradle:test-distribution` is published to Maven Central and integrates with Bitrise Test Distribution.

If you want to pin the dependencies (Gradle dependency verification metadata), pin the activating step to a full patch version in your workflow. The matching CLI version's release page also has `verification-metadata.xml` (and `verification-metadata-mirror.xml` from v2.4.6 onward, for builds where the Bitrise Maven Central mirror is active) attached as assets — use it to seed your `gradle/verification-metadata.xml`.

## Releases
