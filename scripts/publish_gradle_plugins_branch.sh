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
# Set GRADLE_PLUGINS_REF to the branch or tag to test. Unset means
# "use the released plugins" and the script does nothing.
set -euo pipefail

REF="${GRADLE_PLUGINS_REF:-}"
if [[ -z "$REF" ]]; then
  echo "GRADLE_PLUGINS_REF is not set — using the released plugin versions"
  exit 0
fi

# SSH, not HTTPS: bitrise-io/gradle-plugins is private and the build VM has no
# HTTPS credentials for github.com, but every workflow that reaches this script
# has already run activate-ssh-key. Never embed a token in the URL — it would
# land in the logs and in argv.
REPO_URL="${GRADLE_PLUGINS_URL:-git@github.com:bitrise-io/gradle-plugins.git}"
# CLI_REPO_DIR, not BITRISE_SOURCE_DIR: change-workdir rewrites the latter, so by
# the time this runs it can point at the cloned test app instead of the CLI repo.
CLI_REPO="${CLI_REPO_DIR:-${BITRISE_SOURCE_DIR:-.}}"
CONSTS="${CLI_REPO}/internal/consts/consts.go"
WORKDIR="${GRADLE_PLUGINS_DIR:-${CLI_REPO}/_gradle_plugins}"

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
# --branch takes a branch or a tag, never a SHA; GRADLE_PLUGINS_REF is one of
# the first two.
# activate-ssh-key only installs SSH_RSA_PRIVATE_KEY, so a dedicated deploy key
# has to be pointed at explicitly. IdentitiesOnly stops ssh offering the agent's
# other keys first and getting rejected before it reaches this one.
if [[ -n "${GRADLE_PLUGINS_DEPLOY_KEY:-}" ]]; then
  key_file="$(mktemp)"
  # Trapped on EXIT so the key does not outlive this script on a failed clone.
  trap 'rm -f "$key_file"' EXIT
  printf '%s\n' "$GRADLE_PLUGINS_DEPLOY_KEY" > "$key_file"
  chmod 600 "$key_file"
  export GIT_SSH_COMMAND="ssh -i $key_file -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new"
  echo "    using GRADLE_PLUGINS_DEPLOY_KEY"
fi

git clone --depth 1 --branch "$REF" "$REPO_URL" "$WORKDIR"
echo "    at $(git -C "$WORKDIR" rev-parse --short HEAD)"

# -PVERSION_NAME is a project property, so it applies to EVERY module in the
# invocation, not just the one being published. Each plugin depends on :common
# as a project dependency, which Gradle turns into a module coordinate at that
# same version — so `:analytics:publishToMavenLocal -PVERSION_NAME=3.4.1` emits a
# POM requiring common:3.4.1. Publishing common once at its own pinned version
# therefore leaves every dependent pointing at a coordinate that does not exist,
# in mavenLocal or anywhere else, and the build fails resolving the init script's
# classpath.
#
# So common goes out alongside each dependent, at that dependent's version, and
# once more at the version the init script names directly.
publish() {
  local project="$1" version="$2"
  # -PreproduciblePublication matches how the plugins' own CI publishes locally.
  (cd "$WORKDIR" && ./gradlew ":common:publishToMavenLocal" ":${project}:publishToMavenLocal" \
    --stacktrace --console=plain \
    "-PVERSION_NAME=${version}" -PreproduciblePublication=true)
}

# The init script's own `classpath("io.bitrise.gradle:common:<COMMON_VERSION>")`.
(cd "$WORKDIR" && ./gradlew ":common:publishToMavenLocal" \
  --stacktrace --console=plain \
  "-PVERSION_NAME=${COMMON_VERSION}" -PreproduciblePublication=true)

publish cache "$CACHE_VERSION"
publish analytics "$ANALYTICS_VERSION"
publish test-distribution "$TESTDISTRO_VERSION"

echo "=== published gradle-plugins ${REF} to ~/.m2/repository"
find "${HOME}/.m2/repository/io/bitrise/gradle" -name "*.jar" -newermt "-30 minutes" 2>/dev/null | sed 's/^/    /' || true
