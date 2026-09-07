#!/bin/bash
# Asserts a Gradle build authenticated with no credential in its own environment. The plugins
# resolve their token by running the CLI mid-build, so the only thing left for it to find is
# what `activate gradle` pinned to the credential store. That is the shape of a GitHub Actions
# job whose secret is scoped to the activate step, and of any CI that drops the env in between.
#
# Run it after a build launched with both BITRISE_BUILD_CACHE_AUTH_TOKEN and
# BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN stripped from the gradlew process. The invocation API
# assertions that follow in the workflow are the positive half: nothing is uploaded unless the
# plugins authenticated.
#
# Every pattern is anchored to the plugin's own log line: Bitrise echoes the commit message and
# PR body into the build log, so a bare word match can hit a PR that merely discusses it.
#
# Usage: check_gradle_auth_from_pinned_credential.sh <build_log>
set -euo pipefail

BUILD_LOG_FILE="$1"

if grep -qE '^\[Bitrise [^]]*\].*No auth token:' "$BUILD_LOG_FILE"; then
  echo "A plugin could not resolve a token through the CLI — nothing was pinned for it to find ❌"
  grep -m 5 -E '^\[Bitrise [^]]*\].*No auth token:' "$BUILD_LOG_FILE" || true
  exit 1
fi

if grep -qE '^\[Bitrise [^]]*\].*Unauthenticated' "$BUILD_LOG_FILE"; then
  echo "A plugin resolved a token but the backend rejected it ❌"
  grep -m 5 -E '^\[Bitrise [^]]*\].*Unauthenticated' "$BUILD_LOG_FILE" || true
  exit 1
fi

echo "Authenticated from the pinned credential, with no auth env in the build ✅"
