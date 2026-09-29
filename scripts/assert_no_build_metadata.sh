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

# name=value pairs, checked only when the value is actually set in this build.
SENSITIVE_ENVS=(
  BITRISE_APP_SLUG
  BITRISE_BUILD_SLUG
  BITRISE_TRIGGERED_WORKFLOW_ID
  BITRISE_BUILD_CACHE_WORKSPACE_ID
  BITRISE_BUILD_CACHE_AUTH_TOKEN
  BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN
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
    # Short values produce false positives against arbitrary config text.
    if [[ -z "$value" || ${#value} -lt 8 ]]; then
      continue
    fi

    if grep -qF -- "$value" "$file"; then
      echo "ASSERT FAIL: $file contains the value of $name"
      status=1
    fi
  done

  # An empty providerName shadowed the plugin's own CI detection, so a warmed-up
  # VM reported every CI build as local.
  if grep -qF 'providerName.set("")' "$file"; then
    echo "ASSERT FAIL: $file sets an empty providerName"
    status=1
  fi

  # `Bearer ` with nothing after it is a broken header, not an absent one.
  if grep -qE 'authorization="Bearer ?"' "$file"; then
    echo "ASSERT FAIL: $file contains an empty Bearer token"
    status=1
  fi
done

if [[ $status -eq 0 ]]; then
  echo "OK: no build-scoped values found"
fi

exit $status
