#!/bin/bash
# Asserts a Gradle build authenticated with the per-build CI JWT, with no auth token in the
# generated init script. The plugins resolve their own token by running the CLI, so the build
# has to be able to find it: either through BITRISE_BUILD_CACHE_CLI, which `activate gradle`
# exports (the shape a customer on the released step has, since that step installs the CLI
# into /tmp/bin), or on $PATH, which is how a lite-activated VM provides it instead.
#
# Run it in a workflow that clears BITRISE_BUILD_CACHE_AUTH_TOKEN and runs `auth clear`.
# Every pattern is anchored to the plugin's own log line: Bitrise echoes the commit message and
# PR body into the build log, so a bare word match can hit a PR that merely discusses it.
#
# Usage: check_gradle_jwt_auth.sh <build_log>
set -euo pipefail

BUILD_LOG_FILE="$1"
INIT_SCRIPT="$HOME/.gradle/init.d/bitrise-build-cache.init.gradle.kts"

if [[ -n "${BITRISE_BUILD_CACHE_AUTH_TOKEN:-}" ]]; then
  echo "BITRISE_BUILD_CACHE_AUTH_TOKEN is still set — this workflow proves nothing unless it is cleared ❌"
  exit 1
fi

# Only cache and analytics are activated here, and neither takes a token any more.
# test-distribution still does, so this assertion belongs in a workflow that leaves it off.
if grep -q 'authToken' "$INIT_SCRIPT"; then
  echo "The init script still hands the plugins an auth token ❌"
  grep -n 'authToken' "$INIT_SCRIPT"
  exit 1
fi

# The plugins look for the CLI in BITRISE_BUILD_CACHE_CLI first, then on $PATH.
# A full activation exports the variable; a lite one cannot — envman belongs to
# a build — and relies on the VM having installed the binary on $PATH instead.
# Both routes are legitimate; what must not happen is neither.
if [[ -n "${BITRISE_BUILD_CACHE_CLI:-}" ]]; then
  if [[ ! -x "${BITRISE_BUILD_CACHE_CLI}" ]]; then
    echo "BITRISE_BUILD_CACHE_CLI points at ${BITRISE_BUILD_CACHE_CLI}, which is not executable ❌"
    exit 1
  fi
  CLI_FOUND_VIA="BITRISE_BUILD_CACHE_CLI=${BITRISE_BUILD_CACHE_CLI}"
elif CLI_ON_PATH="$(command -v bitrise-build-cache)"; then
  CLI_FOUND_VIA="\$PATH (${CLI_ON_PATH})"
else
  echo "The CLI is neither exported as BITRISE_BUILD_CACHE_CLI nor on \$PATH — the plugins cannot resolve a token ❌"
  exit 1
fi

if grep -qE '^\[Bitrise [^]]*\].*(using the configured token|No auth token:)' "$BUILD_LOG_FILE"; then
  echo "A plugin fell back instead of resolving a token through the CLI ❌"
  grep -E '^\[Bitrise [^]]*\].*(using the configured token|No auth token:)' "$BUILD_LOG_FILE" || true
  exit 1
fi

if ! grep -qE '^\[Bitrise Build Cache\].*Connected to remote server successfully' "$BUILD_LOG_FILE"; then
  echo "Remote cache did not authenticate ❌"
  exit 1
fi

# A JWT carries its own workspace, so the CLI prints it bare and the plugin sends no x-org-id
# header. Seeing that header means a workspaceID:token PAT was used after all.
if grep -qE '^\[Bitrise Build Cache\].*Request metadata:.*x-org-id' "$BUILD_LOG_FILE"; then
  echo "x-org-id header present — a PAT-shaped token was used, not the JWT ❌"
  grep -m 1 -E '^\[Bitrise Build Cache\].*Request metadata:.*x-org-id' "$BUILD_LOG_FILE" || true
  exit 1
fi

echo "Authenticated with the CI JWT via ${CLI_FOUND_VIA}, no token in the init script ✅"
