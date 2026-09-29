#!/usr/bin/env bash
#
# Emulates the preboot VM warmup (build-prebooting-deployments,
# preboot-reconciler/startup_script_extension_*.sh) inside an e2e build.
#
# The point is negative: activation must not be able to see anything the
# monolith injects per build. So every build-scoped variable is stripped from
# the child environment before `activate --lite` runs, and the run is asserted
# to have left no build-scoped value behind on disk.
#
# Usage: preboot_emulate.sh <tool> [extra activate args...]
set -euo pipefail

CLI="${PREBOOT_CLI:?PREBOOT_CLI must point at the built bitrise-build-cache CLI}"
TOOL="${1:?usage: preboot_emulate.sh <tool> [args...]}"
shift

# Everything Bitrise injects per build. If activation can read any of it, the
# workflow is not testing warmup.
BUILD_SCOPED_ENVS=(
  BITRISE_IO
  BITRISE_BUILD_SLUG
  BITRISE_BUILD_NUMBER
  BITRISE_BUILD_URL
  BITRISE_BUILD_API_TOKEN
  BITRISE_APP_SLUG
  BITRISE_APP_TITLE
  BITRISE_TRIGGERED_WORKFLOW_ID
  BITRISE_TRIGGERED_WORKFLOW_TITLE
  BITRISE_STEP_EXECUTION_ID
  BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN
  BITRISE_BUILD_CACHE_AUTH_TOKEN
  BITRISE_BUILD_CACHE_WORKSPACE_ID
  BITRISEIO_BUILD_HUB_VM_TOKEN
  BITRISEIO_BUILD_HUB_VM_TOKEN_URL
  BITRISE_GIT_BRANCH
  BITRISE_GIT_COMMIT
  BITRISE_GIT_MESSAGE
  BITRISE_PULL_REQUEST
  GIT_REPOSITORY_URL
  CI
)

unset_args=()
for name in "${BUILD_SCOPED_ENVS[@]}"; do
  unset_args+=(-u "$name")
done

echo "=== preboot warmup: activate ${TOOL} --lite $*"

# PATH is trimmed too: envman is a build tool and the real warmup never has it.
# Anything the CLI would deliver through envman has to reach the build some
# other way, or it is not actually deferred.
env "${unset_args[@]}" \
  BITRISE_BUILD_CACHE_PREBOOT_EMULATION=true \
  "$CLI" activate "$TOOL" --lite --no-update-check "$@"

echo "=== preboot warmup finished"
