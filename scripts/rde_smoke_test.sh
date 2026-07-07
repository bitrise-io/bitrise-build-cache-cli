#!/usr/bin/env bash
# On-demand smoke test that provisions a Bitrise Remote Dev Environment
# (RDE) mac session, pipes install/installer.sh into it, runs `doctor`,
# then permanently deletes the session.
#
# Runs from the release workflow's peer scope — invoke it after the
# GitHub release is live so the pinned tag resolves cleanly.
#
# Required env:
#   RDE_BITRISE_PAT     — Bitrise PAT with RDE scope.
#   WORKSPACE_SLUG     — target workspace slug.
#   RDE_STACK_ID         — e.g. osx-xcode-26.0.x-edge.
#   RDE_MACHINE_TYPE     — e.g. g2.mac.medium.
#   RDE_SMOKE_CLI_TAG      — CLI tag to smoke, e.g. v3.0.1 (script strips leading v).
#
# Optional:
#   RDE_API_BASE         — override for the RDE API root; defaults to
#                          https://api.bitrise.io/rde
#   RDE_AUTO_TERMINATE_MIN
#                        — session auto-terminate. Defaults to 30 min so a
#                          crashed script can't leak a session for hours.
#   RDE_CLUSTER          — required only when the stack + machine pair is
#                          served by multiple clusters (API returns
#                          "cluster is ambiguous").

set -euo pipefail

: "${RDE_BITRISE_PAT:?}"
: "${WORKSPACE_SLUG:?}"
: "${RDE_STACK_ID:?}"
: "${RDE_MACHINE_TYPE:?}"
: "${RDE_SMOKE_CLI_TAG:?}"

API_BASE="${RDE_API_BASE:-https://api.bitrise.io/rde}"
AUTO_TERMINATE_MIN="${RDE_AUTO_TERMINATE_MIN:-30}"
WS_PATH="/v1/workspaces/${WORKSPACE_SLUG}"

for tool in jq curl sshpass ssh; do
  command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 1; }
done

log() { printf '[rde-smoke] %s\n' "$*"; }

curl_rde() {
  local method="$1" path="$2"; shift 2
  local tmp status
  tmp=$(mktemp)
  status=$(curl -sS -o "$tmp" -w '%{http_code}' -X "$method" \
    -H "Authorization: Bearer $RDE_BITRISE_PAT" \
    -H "X-Request-Source: cli" \
    -H "User-Agent: bitrise-build-cache-cli-rde-smoke" \
    -H "Content-Type: application/json" \
    "$@" "${API_BASE}${path}")
  if [[ "$status" != 2* ]]; then
    echo "[rde-smoke] HTTP $status on $method ${API_BASE}${path}" >&2
    cat "$tmp" >&2
    echo >&2
    rm -f "$tmp"
    return 22
  fi

  cat "$tmp"
  rm -f "$tmp"
}

# ---------- provision ----------
log "provisioning session on $RDE_STACK_ID / $RDE_MACHINE_TYPE"
create_body=$(jq -n \
  --arg name "cli-smoke-${RDE_SMOKE_CLI_TAG}" \
  --arg desc "Smoke test for CLI ${RDE_SMOKE_CLI_TAG}" \
  --arg stack "$RDE_STACK_ID" \
  --arg mtype "$RDE_MACHINE_TYPE" \
  --arg cluster "${RDE_CLUSTER:-}" \
  --argjson autoterm "$AUTO_TERMINATE_MIN" \
  '{name:$name, description:$desc, stackId:$stack, machineType:$mtype, autoTerminateMinutes:$autoterm}
   + (if $cluster == "" then {} else {cluster:$cluster} end)')

create_resp=$(curl_rde POST "${WS_PATH}/sessions" -d "$create_body")
session_id=$(echo "$create_resp" | jq -r '.session.id // empty')
if [[ -z "$session_id" ]]; then
  echo "provision failed:" >&2
  echo "$create_resp" >&2
  exit 1
fi
log "session id: $session_id"

# Delete on ANY exit — including test failure.
cleanup() {
  local rc=$?
  log "deleting session $session_id (rc=$rc)"
  curl -fsS -X DELETE \
    -H "Authorization: Bearer $RDE_BITRISE_PAT" \
    -H "X-Request-Source: cli" \
    "${API_BASE}${WS_PATH}/sessions/${session_id}" >/dev/null || true
  exit "$rc"
}
trap cleanup EXIT

# ---------- wait for RUNNING + SSH details populated ----------
log "waiting for session to reach SESSION_STATUS_RUNNING + SSH ready"
ssh_addr="" ssh_password=""
for _ in $(seq 1 60); do
  s=$(curl_rde GET "${WS_PATH}/sessions/${session_id}")
  status=$(echo "$s" | jq -r '.session.status // empty')
  ssh_addr=$(echo "$s" | jq -r '.session.sshAddress // empty')
  ssh_password=$(echo "$s" | jq -r '.session.sshPassword // empty')
  if [[ "$status" == "SESSION_STATUS_RUNNING" && -n "$ssh_addr" && -n "$ssh_password" ]]; then
    break
  fi

  sleep 10
done

[[ "$status" == "SESSION_STATUS_RUNNING" ]] || { echo "session never reached RUNNING (last: $status)" >&2; exit 1; }
[[ -n "$ssh_addr" && -n "$ssh_password" ]] || { echo "session RUNNING but SSH details empty" >&2; exit 1; }

# sshAddress is "user@host:port" — split it.
ssh_userhost="${ssh_addr%:*}"
ssh_port="${ssh_addr##*:}"
[[ "$ssh_port" == "$ssh_addr" ]] && ssh_port=22
log "ssh ready: ${ssh_userhost}:${ssh_port}"

# ---------- exec smoke commands ----------
SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -p "$ssh_port")

remote_bash() {
  # Forced-interactive login shell, matching bitrise_devenv_execute semantics.
  SSHPASS="$ssh_password" sshpass -e ssh "${SSH_OPTS[@]}" "$ssh_userhost" "bash -i -l -c $(printf '%q' "$1")"
}

log "[1/3] install CLI via installer.sh"
remote_bash "curl -fsSL https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh | sh -s -- -b \"\$HOME/.bitrise/bin\""

log "[2/3] check --version reports ${RDE_SMOKE_CLI_TAG#v}"
got_version=$(remote_bash "\$HOME/.bitrise/bin/bitrise-build-cache --version" | awk '{print $NF}')
[[ "$got_version" == "${RDE_SMOKE_CLI_TAG#v}"* ]] || {
  echo "version mismatch: want ${RDE_SMOKE_CLI_TAG#v}, got $got_version" >&2
  exit 1
}

log "[3/3] doctor exits 0 on a fresh mac session"
remote_bash "\$HOME/.bitrise/bin/bitrise-build-cache doctor"

log "smoke test passed"
