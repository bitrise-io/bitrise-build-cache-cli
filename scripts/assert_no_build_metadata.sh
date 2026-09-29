#!/usr/bin/env bash
#
# Asserts that a config written by `activate --lite` carries nothing that
# belongs to one build. A warmed-up VM is reused by whatever build lands on it,
# so any of these values baked in would attribute that build to the wrong app,
# or authenticate it as the wrong workspace.
#
# Usage: assert_no_build_metadata.sh <file> [file...]
set -uo pipefail

status=0
# A value check that cannot run proves nothing, and a run of only skipped checks
# used to print OK. Count what actually ran and fail if that is zero.
substantive=0

# name=value pairs, checked only when the value is actually set in this build.
SENSITIVE_ENVS=(
  BITRISE_APP_SLUG
  BITRISE_BUILD_SLUG
  BITRISE_TRIGGERED_WORKFLOW_ID
  BITRISE_BUILD_CACHE_WORKSPACE_ID
  BITRISE_BUILD_CACHE_AUTH_TOKEN
  BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN
)

# Key names that must never appear, whatever the value beside them. These make
# the check non-vacuous on a file whose build-scoped values happen to be unset
# or too short to grep for.
FORBIDDEN_KEYS=(
  externalAppId
  externalBuildId
  externalWorkflowName
  '"authToken"'
)

for file in "$@"; do
  if [[ ! -f "$file" ]]; then
    echo "ASSERT FAIL: $file does not exist"
    status=1
    continue
  fi

  echo "--- checking $file for build-scoped values"

  for name in "${SENSITIVE_ENVS[@]}"; do
    value="${!name:-}"
    if [[ -z "$value" ]]; then
      echo "    SKIPPED $name: not set in this environment"
      continue
    fi
    # Short values produce false positives against arbitrary config text.
    # BITRISE_TRIGGERED_WORKFLOW_ID is routinely "primary" (7 chars).
    if [[ ${#value} -lt 8 ]]; then
      echo "    SKIPPED $name: value is only ${#value} chars, too short to grep for safely"
      continue
    fi

    substantive=$((substantive + 1))
    if grep -qF -- "$value" "$file"; then
      echo "ASSERT FAIL: $file contains the value of $name"
      status=1
    fi
  done

  for key in "${FORBIDDEN_KEYS[@]}"; do
    substantive=$((substantive + 1))
    if grep -qF -- "$key" "$file"; then
      echo "ASSERT FAIL: $file contains the build-scoped key $key"
      status=1
    fi
  done

  # An empty providerName shadowed the plugin's own CI detection, so a warmed-up
  # VM reported every CI build as local. appSlug had the same shape.
  substantive=$((substantive + 1))
  if grep -qE '(providerName|appSlug)\.set\(""\)' "$file"; then
    echo "ASSERT FAIL: $file sets an empty providerName/appSlug"
    status=1
  fi

  # `Bearer ` with nothing after it is a broken header, not an absent one.
  substantive=$((substantive + 1))
  if grep -qE 'authorization="Bearer ?"' "$file"; then
    echo "ASSERT FAIL: $file contains an empty Bearer token"
    status=1
  fi
done

# Only meaningful when nothing else failed; a missing file already reported why.
if [[ $status -eq 0 && $substantive -eq 0 ]]; then
  echo "ASSERT FAIL: every check was skipped, so this assertion proved nothing"
  status=1
fi

if [[ $status -eq 0 ]]; then
  echo "OK: $substantive checks ran, no build-scoped values found"
fi

exit $status
