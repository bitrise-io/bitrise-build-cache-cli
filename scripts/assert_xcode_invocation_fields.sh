#!/usr/bin/env bash
# Asserts on the Bitrise BuildCache invocation-detail JSON for the xcode
# build tool. Used by the xcode e2e workflows to verify that recently-
# shipped features (xcode version/build number capture, xcactivitylog
# enrichment re-PUT) actually surface on the BE.
#
# Requires the following env vars, matching the pattern used by
# _e2e/scripts/assert_invocations.sh:
#   MONOLITH_API_PAT — Bitrise PAT with :view_build_cache on WORKSPACE_SLUG
#   WORKSPACE_SLUG   — target workspace slug
#
# The BE's BuildToolInvocationInfoPresenter JSON key names are not
# stable enough across releases to hard-code. The helpers below accept
# both snake_case and camelCase variants for each field and pass if any
# variant is present and non-empty. The full detail JSON is echoed on
# failure so post-mortems have the raw shape available.

# NOTE: no top-level 'set' — this file is sourced into caller shells;
# per-function safety is enough.

readonly BITRISE_API_BASE="${BITRISE_API_BASE:-https://app.bitrise.io}"

_bbc_fetch_xcode_invocation_detail() {
  local invocation_id="$1"
  local url="${BITRISE_API_BASE}/build-cache/${WORKSPACE_SLUG}/invocations/xcode/${invocation_id}.json"

  local response
  if ! response=$(curl -sSf \
    -H "Authorization: token ${MONOLITH_API_PAT}" \
    -H "Accept: application/json" \
    "$url"); then
    echo "FAIL: could not GET $url" >&2

    return 1
  fi

  printf '%s' "$response"
}

# First non-empty value across a list of jq paths. Empty string if none match.
_bbc_first_nonempty() {
  local json="$1"
  shift

  local path value
  for path in "$@"; do
    value=$(printf '%s' "$json" | jq -r "$path // \"\"" 2>/dev/null || true)
    if [[ -n "$value" && "$value" != "null" ]]; then
      printf '%s' "$value"

      return 0
    fi
  done

  return 0
}

# assert_xcode_invocation_enriched <invocation_id>
# Asserts command, shortCommand, xcodeVersion, and toolBuildNumber are all
# non-empty on the BE detail payload. Non-empty command acts as the proxy for
# "the enrichment re-PUT landed" — slim emit never sets command; only the
# enricher does. The BE folds scheme into command/shortCommand and does not
# expose it as a standalone field, so scheme presence is not checked directly.
assert_xcode_invocation_enriched() {
  local invocation_id="$1"
  if [[ -z "$invocation_id" ]]; then
    echo "FAIL: assert_xcode_invocation_enriched requires an invocation id" >&2

    return 1
  fi

  local json
  json=$(_bbc_fetch_xcode_invocation_detail "$invocation_id")

  local full_command short_command xcode_version tool_build_number
  full_command=$(_bbc_first_nonempty "$json" '.command')
  short_command=$(_bbc_first_nonempty "$json" '.shortCommand' '.short_command')
  xcode_version=$(_bbc_first_nonempty "$json" '.xcodeVersion' '.xcode_version' '.toolVersion' '.tool_version')
  tool_build_number=$(_bbc_first_nonempty "$json" '.toolBuildNumber' '.tool_build_number' '.xcodeBuildNumber' '.xcode_build_number')

  local failed=0
  if [[ -z "$full_command" ]]; then
    echo "FAIL: BE detail for $invocation_id has empty command (enrichment re-PUT missing)" >&2
    failed=1
  else
    echo "OK: command=$full_command"
  fi

  if [[ -z "$short_command" ]]; then
    echo "FAIL: BE detail for $invocation_id has empty shortCommand" >&2
    failed=1
  else
    echo "OK: shortCommand=$short_command"
  fi

  if [[ -z "$xcode_version" ]]; then
    echo "FAIL: BE detail for $invocation_id has empty xcodeVersion" >&2
    failed=1
  else
    echo "OK: xcodeVersion=$xcode_version"
  fi

  if [[ -z "$tool_build_number" ]]; then
    echo "FAIL: BE detail for $invocation_id has empty toolBuildNumber" >&2
    failed=1
  else
    echo "OK: toolBuildNumber=$tool_build_number"
  fi

  if [[ $failed -ne 0 ]]; then
    echo "--- BE detail payload for $invocation_id ---" >&2
    printf '%s' "$json" | jq '.' >&2 || printf '%s\n' "$json" >&2

    return 1
  fi
}

# assert_xcode_invocation_hit_rate_at_least <invocation_id> <min_percent>
# Reads hitRate off the BE detail and asserts it >= min_percent (0-100).
# Used by the two-checkout CAS-key-stability workflow to prove the second
# build reuses uploads from the first, independent of absolute path.
assert_xcode_invocation_hit_rate_at_least() {
  local invocation_id="$1"
  local min_percent="$2"
  if [[ -z "$invocation_id" || -z "$min_percent" ]]; then
    echo "FAIL: assert_xcode_invocation_hit_rate_at_least <invocation_id> <min_percent>" >&2

    return 1
  fi

  local json
  json=$(_bbc_fetch_xcode_invocation_detail "$invocation_id")

  local hit_rate
  hit_rate=$(_bbc_first_nonempty "$json" '.hitRate' '.hit_rate')
  if [[ -z "$hit_rate" ]]; then
    echo "FAIL: BE detail for $invocation_id has no hitRate field" >&2
    printf '%s' "$json" | jq '.' >&2 || printf '%s\n' "$json" >&2

    return 1
  fi

  # hitRate is a float on [0, 1]. Convert to integer percent for a portable compare.
  local percent
  percent=$(awk -v r="$hit_rate" 'BEGIN { printf "%d", (r * 100) + 0.5 }')

  if [[ "$percent" -lt "$min_percent" ]]; then
    echo "FAIL: hitRate=${hit_rate} (${percent}%) < required ${min_percent}%" >&2
    printf '%s' "$json" | jq '.' >&2 || printf '%s\n' "$json" >&2

    return 1
  fi

  echo "OK: hitRate=${hit_rate} (${percent}%) >= ${min_percent}%"
}

# assert_xcode_no_orphan_invocations_for_build <build_slug> <expected_invocation_id>
# Lists xcode invocations for the current Bitrise build_slug and asserts that
# exactly one xcode invocation exists, and its id matches <expected_invocation_id>.
# Used by the F1/F2 race workflow: if the enrichment watcher (F2) had minted an
# orphan (unmatched manifest → new UUID) instead of waiting for the slim-record
# retry bucket, the BE would show TWO xcode invocations under the same build.
assert_xcode_no_orphan_invocations_for_build() {
  local build_slug="$1"
  local expected_id="$2"
  if [[ -z "$build_slug" || -z "$expected_id" ]]; then
    echo "FAIL: assert_xcode_no_orphan_invocations_for_build <build_slug> <expected_invocation_id>" >&2

    return 1
  fi

  local url="${BITRISE_API_BASE}/build-cache/${WORKSPACE_SLUG}/invocations.json?tool=xcode&build_slug=${build_slug}&items_per_page=100"

  local response
  if ! response=$(curl -sSf \
    -H "Authorization: token ${MONOLITH_API_PAT}" \
    -H "Accept: application/json" \
    "$url"); then
    echo "FAIL: could not GET $url" >&2

    return 1
  fi

  local ids count
  ids=$(printf '%s' "$response" | jq -r '.items[]? | (.invocationId // .invocation_id // "")' | grep -v '^$' || true)
  if [[ -z "$ids" ]]; then
    count=0
  else
    count=$(printf '%s\n' "$ids" | wc -l | tr -d '[:space:]')
  fi

  if [[ "$count" -ne 1 ]]; then
    echo "FAIL: expected exactly 1 xcode invocation for build ${build_slug}, got ${count}" >&2
    echo "invocation ids seen:" >&2
    printf '%s\n' "$ids" >&2
    echo "--- BE list payload ---" >&2
    printf '%s' "$response" | jq '.' >&2 || printf '%s\n' "$response" >&2

    return 1
  fi

  if [[ "$ids" != "$expected_id" ]]; then
    echo "FAIL: single xcode invocation id ${ids} does not match wrapper id ${expected_id}" >&2
    echo "--- BE list payload ---" >&2
    printf '%s' "$response" | jq '.' >&2 || printf '%s\n' "$response" >&2

    return 1
  fi

  echo "OK: exactly one xcode invocation for build ${build_slug}, id=${expected_id}"
}
