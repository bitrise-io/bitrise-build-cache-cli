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
# Asserts scheme, fullCommand, xcodeVersion, and toolBuildNumber are all
# non-empty on the BE detail payload. Non-empty scheme also acts as a proxy
# for "the enrichment re-PUT landed" — slim emit never sets scheme.
assert_xcode_invocation_enriched() {
  local invocation_id="$1"
  if [[ -z "$invocation_id" ]]; then
    echo "FAIL: assert_xcode_invocation_enriched requires an invocation id" >&2

    return 1
  fi

  local json
  json=$(_bbc_fetch_xcode_invocation_detail "$invocation_id")

  local scheme full_command xcode_version tool_build_number
  scheme=$(_bbc_first_nonempty "$json" '.scheme' '.schemeName' '.scheme_name')
  full_command=$(_bbc_first_nonempty "$json" '.fullCommand' '.full_command')
  xcode_version=$(_bbc_first_nonempty "$json" '.xcodeVersion' '.xcode_version' '.toolVersion' '.tool_version')
  tool_build_number=$(_bbc_first_nonempty "$json" '.toolBuildNumber' '.tool_build_number' '.xcodeBuildNumber' '.xcode_build_number')

  local failed=0
  if [[ -z "$scheme" ]]; then
    echo "FAIL: BE detail for $invocation_id has empty scheme (enrichment re-PUT missing)" >&2
    failed=1
  else
    echo "OK: scheme=$scheme"
  fi

  if [[ -z "$full_command" ]]; then
    echo "FAIL: BE detail for $invocation_id has empty fullCommand" >&2
    failed=1
  else
    echo "OK: fullCommand=$full_command"
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
