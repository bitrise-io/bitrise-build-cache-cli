#!/usr/bin/env bash
# Verify that the invocations the CLI claims to have submitted are actually
# visible through the Monolith BuildCache invocations API. The matching
# `react_e2e_assert_analytics.sh` script already covers the CLI-side logs;
# this one closes the loop by querying the backend.
set -uo pipefail

: "${RN_CLI_LOG:?RN_CLI_LOG must point at the CLI output log}"
: "${MONOLITH_API_PAT:?MONOLITH_API_PAT (Bitrise PAT) must be set}"
: "${WORKSPACE_SLUG:?WORKSPACE_SLUG must be set}"

BASE_URL="${MONOLITH_API_BASE_URL:-https://app.bitrise.io}"
RETRIES="${BE_QUERY_RETRIES:-6}"
RETRY_DELAY="${BE_QUERY_RETRY_DELAY:-5}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required for BE assertions ❌"
  exit 1
fi

failures=0

ok()   { printf '✅ %s\n' "$*"; }
fail() { printf '❌ %s\n' "$*"; failures=$((failures + 1)); }
note() { printf 'ℹ️  %s\n' "$*"; }

# extract_log_id <prefix> [<file>...]
extract_log_id() {
  local prefix=$1
  shift
  grep -hoE "${prefix}: [a-zA-Z0-9-]+" "$@" 2>/dev/null | head -1 | awk '{print $NF}'
}

# fetch_be <build_tool> <invocation_id> <out_file>
# Polls BE until the invocation appears or RETRIES is exhausted.
fetch_be() {
  local build_tool=$1 invocation_id=$2 out=$3
  local url="${BASE_URL}/build-cache/${WORKSPACE_SLUG}/invocations/${build_tool}/${invocation_id}.json"
  local attempt code

  for attempt in $(seq 1 "$RETRIES"); do
    code=$(curl --silent --show-error -o "$out" -w '%{http_code}' \
      -H "Authorization: token ${MONOLITH_API_PAT}" \
      -H "Accept: application/json" \
      "$url" || echo "000")
    if [ "$code" = "200" ]; then
      return 0
    fi
    note "GET ${build_tool}/${invocation_id} attempt ${attempt}/${RETRIES}: HTTP ${code}, retrying in ${RETRY_DELAY}s"
    sleep "$RETRY_DELAY"
  done

  return 1
}

# fetch_be_children <build_tool> <invocation_id> <out_file>
fetch_be_children() {
  local build_tool=$1 invocation_id=$2 out=$3
  local url="${BASE_URL}/build-cache/${WORKSPACE_SLUG}/invocations/${build_tool}/${invocation_id}/child-invocations.json"
  local code
  code=$(curl --silent --show-error -o "$out" -w '%{http_code}' \
    -H "Authorization: token ${MONOLITH_API_PAT}" \
    -H "Accept: application/json" \
    "$url" || echo "000")
  [ "$code" = "200" ]
}

# assert_field <label> <expected> <actual>
assert_field() {
  local label=$1 expected=$2 actual=$3
  if [ "$expected" = "$actual" ]; then
    ok "${label}: ${actual}"
  else
    fail "${label}: expected '${expected}', got '${actual}'"
  fi
}

# assert_present <label> <value>
assert_present() {
  local label=$1 value=$2
  if [ -n "$value" ] && [ "$value" != "null" ]; then
    ok "${label} present: ${value}"
  else
    fail "${label} missing or null"
  fi
}

# --- Discover invocation IDs from CLI / xcelerate logs --------------------

rn_id=$(extract_log_id "React Native invocation ID" "$RN_CLI_LOG")
ccache_id=$(extract_log_id "Ccache invocation ID" "$RN_CLI_LOG")

XCELERATE_LOGS=$(find "${BITRISE_DEPLOY_DIR:-.}" -name 'xcelerate-*.log' 2>/dev/null || true)
xcode_id=""
if [ -n "$XCELERATE_LOGS" ]; then
  # The xcode invocation ID is logged as "Registering invocation relation: parent=<rn>, child=<xcode>, build-tool=xcode".
  xcode_id=$(grep -hoE "child=[a-zA-Z0-9-]+, build-tool=xcode" $XCELERATE_LOGS 2>/dev/null \
    | head -1 \
    | sed -E 's/^child=([a-zA-Z0-9-]+).*/\1/')
fi

if [ -z "$rn_id" ]; then
  fail "Could not find React Native invocation ID in CLI log"
  echo "Aborting BE assertions — no parent ID to query."
  exit 1
fi
ok "Discovered React Native invocation ID: $rn_id"
[ -n "$ccache_id" ] && note "Discovered ccache invocation ID: $ccache_id"
[ -n "$xcode_id" ]  && note "Discovered xcode invocation ID: $xcode_id"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

# --- 1. Parent invocation visible on BE -----------------------------------

rn_body="${tmpdir}/rn.json"
if fetch_be "react-native" "$rn_id" "$rn_body"; then
  ok "GET react-native/${rn_id} returned 200"

  body_id=$(jq -r '.id // empty' "$rn_body")
  assert_field "react-native invocation id" "$rn_id" "$body_id"

  # Expected fields proving the record looks right.
  assert_present "buildTool"   "$(jq -r '.buildTool // empty' "$rn_body")"
  assert_present "command"     "$(jq -r '.command // empty' "$rn_body")"
  assert_present "status"      "$(jq -r '.status // empty' "$rn_body")"
  assert_present "workflow"    "$(jq -r '.workflow // empty' "$rn_body")"
  assert_present "projectSlug" "$(jq -r '.projectSlug // empty' "$rn_body")"

  # Cache stats — proves analytics (not just registration) reached BE.
  hits=$(jq -r '.cacheHits // .stats.cacheHits // empty' "$rn_body")
  misses=$(jq -r '.cacheMisses // .stats.cacheMisses // empty' "$rn_body")
  if [ -n "$hits$misses" ]; then
    ok "cache stats present (hits=${hits:-0}, misses=${misses:-0})"
  else
    fail "no cache stats fields found on react-native invocation"
  fi
else
  fail "react-native invocation ${rn_id} not visible on BE after ${RETRIES} attempts"
  echo "Last response body:"
  cat "$rn_body" 2>/dev/null || true
fi

# --- 2. Child invocations registered (when applicable) --------------------

children_body="${tmpdir}/children.json"
expect_children=false
[ -n "$ccache_id" ] && expect_children=true
[ -n "$xcode_id" ]  && expect_children=true

if [ "$expect_children" = "true" ]; then
  if fetch_be_children "react-native" "$rn_id" "$children_body"; then
    ok "GET react-native/${rn_id}/child-invocations returned 200"

    if [ -n "$ccache_id" ]; then
      if jq -e --arg id "$ccache_id" \
        '[.[] | select(.buildTool == "ccache") | .invocations[]?.id] | index($id)' \
        "$children_body" >/dev/null; then
        ok "ccache child ${ccache_id} attached to RN parent on BE"
      else
        fail "ccache child ${ccache_id} not attached to RN parent on BE"
        jq '.' "$children_body" || cat "$children_body"
      fi
    fi

    if [ -n "$xcode_id" ]; then
      if jq -e --arg id "$xcode_id" \
        '[.[] | select(.buildTool == "xcode") | .invocations[]?.id] | index($id)' \
        "$children_body" >/dev/null; then
        ok "xcode child ${xcode_id} attached to RN parent on BE"
      else
        fail "xcode child ${xcode_id} not attached to RN parent on BE"
        jq '.' "$children_body" || cat "$children_body"
      fi
    fi
  else
    fail "child-invocations endpoint did not return 200 for ${rn_id}"
    cat "$children_body" 2>/dev/null || true
  fi
else
  note "No ccache/xcode child detected, skipping child-invocation BE assertion"
fi

# --- Result ---------------------------------------------------------------

if [ "$failures" -gt 0 ]; then
  echo "BE invocation assertions: ${failures} failure(s) ❌"
  exit 1
fi

echo "BE invocation assertions all passed ✅"
