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

# --- Discover invocation ID + locally-active tools -----------------------

rn_id=$(grep -oE "React Native invocation ID: [a-zA-Z0-9-]+" "$RN_CLI_LOG" | head -1 | awk '{print $NF}')

ccache_active=false
xcode_active=false
if grep -q "Ccache invocation ID:" "$RN_CLI_LOG"; then
  ccache_active=true
fi
if find "${BITRISE_DEPLOY_DIR:-.}" -name 'xcelerate-*.log' -print -quit 2>/dev/null | grep -q .; then
  xcode_active=true
fi

if [ -z "$rn_id" ]; then
  fail "Could not find React Native invocation ID in CLI log"
  echo "Aborting BE assertions — no parent ID to query."
  exit 1
fi
ok "Discovered React Native invocation ID: $rn_id"
$ccache_active && note "ccache was active in this run"
$xcode_active  && note "xcode (xcelerate) was active in this run"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

# --- 1. Parent invocation visible on BE ----------------------------------
# Field names match BuildToolInvocationInfoPresenter#to_h:
#   invocationId, tool, command, status, workflowName, projectSlug, cacheHitRate

rn_body="${tmpdir}/rn.json"
if fetch_be "react-native" "$rn_id" "$rn_body"; then
  ok "GET react-native/${rn_id} returned 200"

  body_id=$(jq -r '.invocationId // empty' "$rn_body")
  assert_field "react-native invocationId" "$rn_id" "$body_id"

  body_tool=$(jq -r '.tool // empty' "$rn_body")
  assert_field "tool" "react-native" "$body_tool"

  assert_present "command"      "$(jq -r '.command // empty' "$rn_body")"
  assert_present "status"       "$(jq -r '.status // empty' "$rn_body")"
  assert_present "workflowName" "$(jq -r '.workflowName // empty' "$rn_body")"
  assert_present "projectSlug"  "$(jq -r '.projectSlug // empty' "$rn_body")"

  # cacheHitRate is a 0..1 float. Accept any numeric value (including 0) as
  # proof analytics arrived; null/missing means stats never reached BE.
  if jq -e '.cacheHitRate | type == "number"' "$rn_body" >/dev/null 2>&1; then
    rate=$(jq -r '.cacheHitRate' "$rn_body")
    ok "cacheHitRate present: ${rate}"
  else
    fail "cacheHitRate missing or non-numeric on react-native invocation"
  fi
else
  fail "react-native invocation ${rn_id} not visible on BE after ${RETRIES} attempts"
  echo "Last response body:"
  cat "$rn_body" 2>/dev/null || true
fi

# --- 2. Child invocations registered (when applicable) ------------------
# Children endpoint shape:
#   [ { "buildTool": "xcode", "invocations": [{ "invocationId": "...", ... }] }, ... ]

if $ccache_active || $xcode_active; then
  children_body="${tmpdir}/children.json"
  if fetch_be_children "react-native" "$rn_id" "$children_body"; then
    ok "GET react-native/${rn_id}/child-invocations returned 200"

    if $xcode_active; then
      xcode_count=$(jq '[.[] | select(.buildTool == "xcode") | .invocations[]?] | length' "$children_body")
      if [ "${xcode_count:-0}" -gt 0 ]; then
        ok "xcode child invocations attached on BE (count=${xcode_count})"
      else
        fail "no xcode child invocations attached to RN parent on BE"
        jq '.' "$children_body" || cat "$children_body"
      fi
    fi

    if $ccache_active; then
      ccache_count=$(jq '[.[] | select(.buildTool == "ccache") | .invocations[]?] | length' "$children_body")
      if [ "${ccache_count:-0}" -gt 0 ]; then
        ok "ccache child invocations attached on BE (count=${ccache_count})"
      else
        fail "no ccache child invocations attached to RN parent on BE"
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

# --- Result --------------------------------------------------------------

if [ "$failures" -gt 0 ]; then
  echo "BE invocation assertions: ${failures} failure(s) ❌"
  exit 1
fi

echo "BE invocation assertions all passed ✅"
