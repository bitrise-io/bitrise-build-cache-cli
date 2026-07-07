#!/bin/bash
set -euo pipefail

CLI=${CLI:-bitrise-build-cache}
FAKE_TOKEN=${FAKE_TOKEN:-bitpat_fake-token-for-ci-e2e}
FAKE_WS=${FAKE_WS:-fake-workspace-id}

log()  { printf '\n\033[36m▶ %s\033[0m\n' "$*"; }
fail() { printf '\n\033[31m✗ %s\033[0m\n' "$*" >&2; exit 1; }
pass() { printf '\033[32m  ✓ %s\033[0m\n' "$*"; }

unset BITRISE_BUILD_CACHE_AUTH_TOKEN BITRISE_BUILD_CACHE_WORKSPACE_ID BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN
security delete-generic-password -s bitrise-build-cache -a default 2>/dev/null || true
rm -rf ~/.bitrise/cache/ ~/.local/state/bitrise-build-cache/ ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts

log "auth set / status / token / clear"
"$CLI" auth set --token "$FAKE_TOKEN" --workspace-id "$FAKE_WS"
STATUS_OUT=$("$CLI" auth status 2>&1)
echo "$STATUS_OUT" | grep -q "$FAKE_WS" || fail "auth status missing workspace (got: $STATUS_OUT)"
if echo "$STATUS_OUT" | grep -qi "Local invocation display name"; then
  pass "auth status surfaces username (ACI-5180 landed)"
else
  printf '\033[33m  ⚠ auth status missing username line — expected until ACI-5180 (PR #417) merges\033[0m\n'
fi
TOKEN_OUT=$("$CLI" auth token 2>&1)
echo "$TOKEN_OUT" | grep -q "^${FAKE_WS}:${FAKE_TOKEN}$" || fail "auth token payload mismatch (got: $TOKEN_OUT)"
"$CLI" auth clear
STATUS_OUT=$("$CLI" auth status 2>&1)
echo "$STATUS_OUT" | grep -qi "not configured\|no credentials" || fail "auth clear did not reset (got: $STATUS_OUT)"
pass "auth CRUD ok"

log "non-TTY interactive rejection"
if "$CLI" activate --interactive </dev/null 2>&1 | grep -qi "interactive setup requires a terminal"; then
  pass "non-TTY error surfaced"
else
  fail "expected non-TTY error"
fi

"$CLI" auth set --token "$FAKE_TOKEN" --workspace-id "$FAKE_WS" >/dev/null

log "activate gradle → sidecar + init.d script + no plaintext token"
"$CLI" activate gradle --push=false >/dev/null
[ -f ~/.bitrise/cache/gradle/config.json ]  || fail "gradle sidecar missing"
[ -f ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts ] || fail "gradle init.d missing"
if grep -q "$FAKE_TOKEN" ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts; then
  fail "plaintext token leaked into gradle init"
fi
pass "gradle sidecar ok, no plaintext token"

log "activate bazel → sidecar + .bazelrc + no plaintext token"
"$CLI" activate bazel --push=false >/dev/null || true
[ -f ~/.bitrise/cache/bazel/config.json ] || fail "bazel sidecar missing"
if [ -f ~/.bazelrc ] && grep -q "$FAKE_TOKEN" ~/.bazelrc; then
  fail "plaintext token leaked into .bazelrc"
fi
pass "bazel sidecar ok"

log "doctor --no-backend-probe --no-update-check --json"
DOCTOR_JSON=$("$CLI" doctor --no-backend-probe --no-update-check --json 2>&1 | sed -n '/^{/,/^}/p')
if [ -z "$DOCTOR_JSON" ]; then fail "doctor produced no JSON"; fi
echo "$DOCTOR_JSON" | jq -e '.overall' >/dev/null || fail "doctor JSON missing overall"
pass "doctor JSON contract ok (overall=$(echo "$DOCTOR_JSON" | jq -r .overall))"

log "drift-nudge fires after simulated CLI-version bump"
mkdir -p ~/.local/state/bitrise-build-cache
"$CLI" doctor --no-backend-probe --no-update-check >/dev/null 2>&1 || true
if [ -f ~/.local/state/bitrise-build-cache/version-state.json ]; then
  jq '.configVersion = "0.0.1"' ~/.bitrise/cache/gradle/config.json > /tmp/g && mv /tmp/g ~/.bitrise/cache/gradle/config.json
  jq '.last_version = "v0.0.1"' ~/.local/state/bitrise-build-cache/version-state.json > /tmp/vs && mv /tmp/vs ~/.local/state/bitrise-build-cache/version-state.json
  if "$CLI" auth status 2>&1 | grep -qi "schema major bump\|re-run.*activate gradle"; then
    pass "drift nudge fires"
  else
    fail "drift nudge did not fire"
  fi
else
  printf '\033[33m  ⚠ version-state.json not created (skipping drift-nudge check)\033[0m\n'
fi

log "update --dry-run detects install method and makes no changes"
BEFORE=$(stat -f %m "$(command -v "$CLI")" 2>/dev/null || stat -c %Y "$(command -v "$CLI")")
"$CLI" update --dry-run | grep -qi "detected install method\|dry run" || fail "update --dry-run output missing"
AFTER=$(stat -f %m "$(command -v "$CLI")" 2>/dev/null || stat -c %Y "$(command -v "$CLI")")
[ "$BEFORE" = "$AFTER" ] || fail "update --dry-run mutated binary"
pass "update --dry-run ok"

log "browse --print / --json"
"$CLI" browse --print | grep -q "$FAKE_WS" || fail "browse --print missing workspace"
"$CLI" browse --json | jq -e --arg ws "$FAKE_WS" '.workspace_id == $ws' >/dev/null || fail "browse --json workspace_id mismatch"
"$CLI" browse "test-inv-id" --print | grep -q "test-inv-id" || fail "browse deep-link missing invocation id"
pass "browse ok"

printf '\n\033[32mAll portable e2e scenarios passed.\033[0m\n'
