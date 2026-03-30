# Justification: Bitrise CLI as a Wrapper Around Build Executables

## Overview

The `bitrise-build-cache` CLI acts as a thin wrapper around existing build executables:

- `./gradlew` (Android / Gradle builds)
- `xcodebuild` (iOS / Xcode builds)
- `ccache` (C++ / native module builds)

All original parameters are passed through to the underlying executable unchanged. The user experience is identical to calling these tools directly.

React Native projects use a wide variety of tooling to trigger these executables, for example:

- `npm run android` / `yarn android` / `pnpm android` — invokes Gradle under the hood
- `react-native run-ios` / `expo run:ios` — invokes xcodebuild under the hood
- `fastlane` lanes — may call any combination of Gradle, xcodebuild, and native C++ compilation

Regardless of which tool the developer uses, the wrapper intercepts at the executable level, so all of these are covered.

## Why a Wrapper?

### 1. Invocation ID

The single most important reason to wrap these executables is the ability to generate a **unique invocation ID each time the wrapper is called**, before execution begins.

Every invocation of the wrapper produces a fresh ID. This invocation ID can be:
- Attached to every cache request made during the build
- Passed as metadata to the build cache backend
- Used to correlate all cache hits/misses, uploads, and downloads from that specific build invocation
- Surfaced in dashboards and analytics to trace the full lifecycle of one build

Without the wrapper, there is no reliable point at which to generate and inject this ID across all three build systems consistently.

### 2. Consistent Authentication & Configuration

The wrapper handles auth token resolution (`BITRISE_BUILD_CACHE_AUTH_TOKEN`, `BITRISE_BUILD_CACHE_WORKSPACE_ID`) once, in one place, before delegating to any executable. Each build system does not need to implement this independently.

### 3. Unified Observability

By controlling the entry point, the wrapper can:
- Record build start/end times
- Capture exit codes
- Emit structured telemetry tied to the invocation ID

### 4. Per-Tool Analytics

Analytics can be individually activated for each command or tool used (Gradle, Xcode, ccache). This is beneficial because it allows fine-grained visibility into cache performance per build system, making it possible to measure hit rates, latency, and savings independently for each tool within the same invocation.

### 4. Cross-System Activation in One Step

For React Native projects that use Gradle, Xcode, and ccache simultaneously, the wrapper (`activate react-native`) activates all three caches in a single command, ensuring they are all configured consistently for the same invocation.

## What the Wrapper Does NOT Do

- It does **not** modify the behavior of the underlying build tools
- It does **not** intercept or alter build outputs
- It does **not** require changes to build scripts or CI configuration beyond the single activation step

## Summary

| Concern | Without Wrapper | With Wrapper |
|---|---|---|
| Invocation ID | Not possible | Generated fresh per wrapper call, shared across all tools |
| Auth configuration | Per-tool, duplicated | Centralized |
| Observability | Fragmented | Unified per invocation |
| Activation | Multiple steps | Single command |

## Usage Examples

The wrapper is called **in place of** the underlying build tool, with all arguments passed through unchanged.

### Wrapping Gradle

```sh
# Instead of:
./gradlew assembleDebug

# Use the wrapper:
bitrise-build-cache ./gradlew assembleDebug
```

### Wrapping xcodebuild

```sh
# Instead of:
xcodebuild -scheme MyApp -configuration Release build

# Use the wrapper:
bitrise-build-cache xcodebuild -scheme MyApp -configuration Release build
```

### Wrapping ccache (C++ native modules)

```sh
# Instead of:
ccache g++ -o output main.cpp

# Use the wrapper:
bitrise-build-cache ccache g++ -o output main.cpp
```

In each case, the wrapper generates a fresh invocation ID, handles authentication, enables analytics, and then delegates to the underlying tool with all original arguments intact.
