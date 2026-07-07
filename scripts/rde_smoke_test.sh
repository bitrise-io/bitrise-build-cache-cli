#!/usr/bin/env bash
# On-demand smoke test that provisions a Bitrise RDE session on a mac
# stack, pipes install/installer.sh into it, runs `doctor`, then tears
# the session down.
#
# Runs from the release workflow's peer scope — invoke it after the
# GitHub release is live so the pinned tag resolves cleanly.
#
# Required env:
#   RDE_ACCESS_TOKEN     — Bitrise PAT scoped to devenv:create/execute/terminate.
#   RDE_WORKSPACE_ID     — target workspace slug.
#   RDE_STACK_ID         — e.g. osx-xcode-26.0.x-edge (from bitrise_devenv_list_stacks).
#   RDE_MACHINE_TYPE     — e.g. g2.mac.medium (from bitrise_devenv_list_machine_types).
#   BITRISE_GIT_TAG      — CLI tag to smoke, e.g. v3.0.1 (script strips leading v).
#
# Optional:
#   RDE_API_BASE         — override for the devenv API root; defaults to
#                          https://api.bitrise.io/v0.1  (⚠ verify path segment;
#                          the endpoint below is the current best guess.)
#   RDE_AUTO_TERMINATE_MIN
#                        — session auto-terminate. Defaults to 30 min so a
#                          crashed script can't leak a session for hours.

set -euo pipefail

: "${RDE_ACCESS_TOKEN:?}"
: "${RDE_WORKSPACE_ID:?}"
: "${RDE_STACK_ID:?}"
: "${RDE_MACHINE_TYPE:?}"
: "${BITRISE_GIT_TAG:?}"

API_BASE="${RDE_API_BASE:-https://api.bitrise.io/v0.1}"
AUTO_TERMINATE_MIN="${RDE_AUTO_TERMINATE_MIN:-30}"

# ⚠ TODO — verify these paths against the real devenv API before shipping.
# The MCP tool names hint at the resource layout (bitrise_devenv_create,
# bitrise_devenv_get, bitrise_devenv_execute, bitrise_devenv_terminate),
# but the wire endpoints are not yet documented in this repo.
DEVENV_SESSIONS_PATH="/organizations/${RDE_WORKSPACE_ID}/devenv/sessions"

log() { printf '[rde-smoke] %s\n' "$*"; }

curl_bitrise() {
  local method="$1" path="$2"; shift 2
  curl -sS -X "$method" \
    -H "Authorization: token $RDE_ACCESS_TOKEN" \
    -H "Content-Type: application/json" \
    "$@" "${API_BASE}${path}"
}

# ---------- provision ----------
log "provisioning session on $RDE_STACK_ID / $RDE_MACHINE_TYPE"
create_body=$(jq -n \
  --arg name "cli-smoke-${BITRISE_GIT_TAG}" \
  --arg desc "Smoke test for CLI ${BITRISE_GIT_TAG}" \
  --arg stack "$RDE_STACK_ID" \
  --arg mtype "$RDE_MACHINE_TYPE" \
  --argjson autoterm "$AUTO_TERMINATE_MIN" \
  '{name:$name, description:$desc, stack_id:$stack, machine_type:$mtype, auto_terminate_minutes:$autoterm}')

session=$(curl_bitrise POST "$DEVENV_SESSIONS_PATH" -d "$create_body")
session_id=$(echo "$session" | jq -r '.id // .session.id // empty')
if [[ -z "$session_id" ]]; then
  echo "provision failed:" >&2
  echo "$session" >&2
  exit 1
fi
log "session id: $session_id"

# Terminate on ANY exit — including test failure.
cleanup() {
  local rc=$?
  log "tearing down session $session_id (rc=$rc)"
  curl_bitrise DELETE "${DEVENV_SESSIONS_PATH}/${session_id}" >/dev/null || true
  exit "$rc"
}
trap cleanup EXIT

# ---------- wait for running + SSH populated ----------
log "waiting for session to reach 'running' + SSH ready"
for _ in $(seq 1 60); do
  s=$(curl_bitrise GET "${DEVENV_SESSIONS_PATH}/${session_id}")
  status=$(echo "$s" | jq -r '.status // .session.status // empty')
  ssh_host=$(echo "$s" | jq -r '.ssh.host // .session.ssh.host // empty')
  if [[ "$status" == "running" && -n "$ssh_host" ]]; then
    break
  fi

  sleep 10
done

[[ "$status" == "running" ]] || { echo "session never reached running: $status" >&2; exit 1; }

ssh_user=$(echo "$s" | jq -r '.ssh.user // .session.ssh.user // "vagrant"')
ssh_port=$(echo "$s" | jq -r '.ssh.port // .session.ssh.port // 22')
log "ssh ready: ${ssh_user}@${ssh_host}:${ssh_port}"

# ---------- exec smoke commands ----------
# Assumes the caller's ssh key is registered with the workspace or that
# the session exposes a short-lived cert. Adjust once the real auth
# handshake is known.
SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p "$ssh_port")

remote_bash() {
  ssh "${SSH_OPTS[@]}" "${ssh_user}@${ssh_host}" bash -lc "$1"
}

log "[1/3] install CLI via installer.sh"
remote_bash "curl -fsSL https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh | sh -s -- -b \"\$HOME/.bitrise/bin\""

log "[2/3] check --version reports ${BITRISE_GIT_TAG#v}"
got_version=$(remote_bash "\$HOME/.bitrise/bin/bitrise-build-cache --version" | awk '{print $NF}')
[[ "$got_version" == "${BITRISE_GIT_TAG#v}"* ]] || {
  echo "version mismatch: want ${BITRISE_GIT_TAG#v}, got $got_version" >&2
  exit 1
}

log "[3/3] doctor exits 0 on a fresh mac session"
remote_bash "\$HOME/.bitrise/bin/bitrise-build-cache doctor"

log "smoke test passed"
