#!/usr/bin/env bash
#
# Builds a gradle-plugins branch and publishes it to the local Maven repo under
# the exact coordinates the CLI pins, so the generated init script picks it up
# without any version override: `mavenLocal()` is first in its initscript
# repositories, so it shadows the released artifacts of the same version.
#
# That is what makes it possible to iterate on the plugin side from a CLI e2e
# run — change the branch, re-run the workflow, and the build uses the new code.
#
# Set GRADLE_PLUGINS_REF to the branch (or tag, or SHA) to test. Unset means
# "use the released plugins" and the script does nothing.
set -euo pipefail

REF="${GRADLE_PLUGINS_REF:-}"
if [[ -z "$REF" ]]; then
  echo "GRADLE_PLUGINS_REF is not set — using the released plugin versions"
  exit 0
fi

REPO_URL="${GRADLE_PLUGINS_URL:-https://github.com/bitrise-io/gradle-plugins.git}"
CONSTS="${BITRISE_SOURCE_DIR:-.}/internal/consts/consts.go"
WORKDIR="${GRADLE_PLUGINS_DIR:-${BITRISE_SOURCE_DIR:-.}/_gradle_plugins}"

# The versions come out of consts.go rather than being repeated here: publishing
# under a version the CLI does not ask for would silently resolve the released
# artifact instead, and the run would look like it tested the branch.
pinned_version() {
  local const_name="$1"
  local value
  value=$(grep -Eo "${const_name} += +\"[^\"]+\"" "$CONSTS" | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
  if [[ -z "$value" ]]; then
    echo "could not read ${const_name} from ${CONSTS}" >&2
    exit 1
  fi
  echo "$value"
}

COMMON_VERSION=$(pinned_version GradleCommonPluginDepVersion)
CACHE_VERSION=$(pinned_version GradleRemoteBuildCachePluginDepVersion)
ANALYTICS_VERSION=$(pinned_version GradleAnalyticsPluginDepVersion)
TESTDISTRO_VERSION=$(pinned_version GradleTestDistributionPluginDepVersion)

echo "=== gradle-plugins ${REF} -> mavenLocal"
echo "    common=${COMMON_VERSION} remote-cache=${CACHE_VERSION} gradle-analytics=${ANALYTICS_VERSION} test-distribution=${TESTDISTRO_VERSION}"

rm -rf "$WORKDIR"
git clone --depth 1 --branch "$REF" "$REPO_URL" "$WORKDIR"
echo "    at $(git -C "$WORKDIR" rev-parse --short HEAD)"

# Each module carries its own VERSION_NAME, so they are published one at a time.
publish() {
  local project="$1" version="$2"
  # -PreproduciblePublication matches how the plugins' own CI publishes locally.
  (cd "$WORKDIR" && ./gradlew ":${project}:publishToMavenLocal" \
    --stacktrace --console=plain \
    "-PVERSION_NAME=${version}" -PreproduciblePublication=true)
}

publish common "$COMMON_VERSION"
publish cache "$CACHE_VERSION"
publish analytics "$ANALYTICS_VERSION"
publish test-distribution "$TESTDISTRO_VERSION"

echo "=== published gradle-plugins ${REF} to ~/.m2/repository"
find "${HOME}/.m2/repository/io/bitrise/gradle" -name "*.jar" -newermt "-30 minutes" 2>/dev/null | sed 's/^/    /' || true
