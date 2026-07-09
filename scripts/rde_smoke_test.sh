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

# Scenario banner + result reporting.
banner() {
  local title="$1"
  printf '\n'
  printf '════════════════════════════════════════════════════════════════════════════\n'
  printf '  %s\n' "$title"
  printf '════════════════════════════════════════════════════════════════════════════\n'
}

step() {
  local label="$1"
  printf '\n── %s ──────────────────────────────────────────────────────\n' "$label"
}

# Named scenarios drive the trap so we always report which one aborted.
CURRENT_SCENARIO=""
scenario() {
  CURRENT_SCENARIO="$1"
  banner "$1"
}

failed_scenarios=()
scenario_ok() {
  printf '\n✅ %s\n' "$CURRENT_SCENARIO"
  CURRENT_SCENARIO=""
}

curl_rde() {
  local method="$1" path="$2"; shift 2
  # --fail-with-body: exit 22 on non-2xx, body still printed to stdout for context.
  curl --fail-with-body -sS -X "$method" \
    -H "Authorization: Bearer $RDE_BITRISE_PAT" \
    -H "X-Request-Source: cli" \
    -H "User-Agent: bitrise-build-cache-cli-rde-smoke" \
    -H "Content-Type: application/json" \
    "$@" "${API_BASE}${path}"
}

# parse_ssh_addr sets ssh_userhost + ssh_port from a session's sshAddress
# (may be a full `ssh user@host -p PORT` command or bare "host:port").
parse_ssh_addr() {
  local addr="$1"
  ssh_userhost=$(printf '%s\n' "$addr" | grep -oE '[[:alnum:]._-]+@[[:alnum:]._-]+' | tail -1)
  ssh_port=$(printf '%s\n'   "$addr" | grep -oE '\-p[[:space:]]+[0-9]+'          | tail -1 | awk '{print $NF}')
  : "${ssh_port:=22}"
  [[ -n "$ssh_userhost" ]] || { echo "could not parse user@host from sshAddress: $addr" >&2; return 1; }
}

# rde_session_cleanup terminates + waits for TERMINATED + deletes + bulk-reaps.
# See reference_rde_api.md: DELETE on a still-TERMINATING session silently
# leaves it as TERMINATED, which the backend counts toward the CPU quota.
rde_session_cleanup() {
  local sid="$1"
  curl_rde POST "${WS_PATH}/sessions/${sid}/terminate" -d '{}' >/dev/null 2>&1 || true
  local st
  for _ in $(seq 1 12); do
    st=$(curl_rde GET "${WS_PATH}/sessions/${sid}" 2>/dev/null | jq -r '.session.status // empty')
    [[ "$st" == "SESSION_STATUS_TERMINATED" ]] && break
    sleep 5
  done
  curl_rde DELETE "${WS_PATH}/sessions/${sid}"                   >/dev/null 2>&1 || true
  curl_rde POST   "${WS_PATH}/sessions:delete-terminated" -d '{}' >/dev/null 2>&1 || true
}

# ---------- reap orphans ----------
# Best-effort: bulk-delete any lingering TERMINATED sessions from prior
# runs whose /sessions/{id} DELETE raced with backend TERMINATING state.
# The backend appears to count TERMINATED sessions toward the CPU quota,
# so a slow drip of leaks eventually blocks new provisioning.
log "reaping any terminated sessions from prior runs"
curl_rde POST "${WS_PATH}/sessions:delete-terminated" -d '{}' >/dev/null 2>&1 || true

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

cleanup() {
  local rc=$?
  if [[ $rc -ne 0 && -n "$CURRENT_SCENARIO" ]]; then
    printf '\n❌ FAILED IN: %s\n' "$CURRENT_SCENARIO" >&2
  fi

  log "cleaning up session $session_id (rc=$rc)"
  rde_session_cleanup "$session_id"
  exit "$rc"
}
trap cleanup EXIT

# ---------- wait for RUNNING + SSH details populated ----------
log "waiting for session to reach SESSION_STATUS_RUNNING + SSH ready"
ssh_addr="" ssh_password="" prev_status=""
for i in $(seq 1 60); do
  s=$(curl_rde GET "${WS_PATH}/sessions/${session_id}")
  status=$(echo "$s" | jq -r '.session.status // empty')
  ssh_addr=$(echo "$s" | jq -r '.session.sshAddress // empty')
  ssh_password=$(echo "$s" | jq -r '.session.sshPassword // empty')

  if [[ "$status" != "$prev_status" ]]; then
    log "[$i] status: $status"
    prev_status="$status"
  fi

  case "$status" in
  SESSION_STATUS_RUNNING)
    ssh_open=$(echo "$s" | jq -r '.session.sshConnectionOpen // false')
    [[ -n "$ssh_addr" && -n "$ssh_password" && "$ssh_open" == "true" ]] && break
    ;;
  SESSION_STATUS_TERMINATED | SESSION_STATUS_STARTUP_ERROR | SESSION_STATUS_TERMINATING)
    echo "session reached terminal status $status before RUNNING; full record:" >&2
    echo "$s" | jq . >&2
    exit 1
    ;;
  esac

  sleep 10
done

[[ "$status" == "SESSION_STATUS_RUNNING" ]] || { echo "session never reached RUNNING (last: $status)" >&2; exit 1; }
[[ -n "$ssh_addr" && -n "$ssh_password" ]] || { echo "session RUNNING but SSH details empty" >&2; exit 1; }

parse_ssh_addr "$ssh_addr" || exit 1
log "ssh ready: ${ssh_userhost}:${ssh_port}"

# ---------- exec smoke commands ----------
SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=10 -p "$ssh_port")

# Probe port until sshd accepts (backend flips RUNNING before sshd binds).
log "probing sshd on ${ssh_userhost}:${ssh_port}"
for i in $(seq 1 30); do
  if SSHPASS="$ssh_password" sshpass -e ssh "${SSH_OPTS[@]}" -o BatchMode=no "$ssh_userhost" 'true' 2>/dev/null; then
    log "sshd reachable"
    break
  fi

  [[ $i -eq 30 ]] && { echo "sshd never accepted connections" >&2; exit 1; }
  sleep 5
done

remote_bash() {
  # Forced-interactive login shell, matching bitrise_devenv_execute semantics.
  SSHPASS="$ssh_password" sshpass -e ssh "${SSH_OPTS[@]}" "$ssh_userhost" "bash -i -l -c $(printf '%q' "$1")"
}

# Detect remote OS once so scenarios can gate mac-only vs linux-only steps.
REMOTE_OS=$(remote_bash "uname -s" | tr -d '\r' | awk 'NF' | tail -1)
log "remote OS: $REMOTE_OS"
is_mac()   { [[ "$REMOTE_OS" == "Darwin" ]]; }
is_linux() { [[ "$REMOTE_OS" == "Linux"  ]]; }

CLI="\$HOME/.bitrise/bin/bitrise-build-cache --no-update-check"
# On Linux the RDE VM has no secret-service, so keychain is unusable —
# write credentials to ~/.bitrise/env on the VM once (0600) and source
# it in every remote_bash call. Avoids leaking the token into ps output
# via 'env FOO=... cmd' args (previous approach).
if is_linux; then
  remote_bash "install -d -m 0700 \$HOME/.bitrise && umask 077 && \
cat > \$HOME/.bitrise/env <<EOF
export BITRISE_BUILD_CACHE_AUTH_TOKEN='${RDE_BITRISE_PAT}'
export BITRISE_BUILD_CACHE_WORKSPACE_ID='${WORKSPACE_SLUG}'
EOF"
  CLI=". \$HOME/.bitrise/env && $CLI"
fi

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/rde_smoke_scenarios.sh
source "${SCRIPT_DIR}/rde_smoke_scenarios.sh"

printf '\n════════════════════════════════════════════════════════════════════════════\n'
printf '  🎉 ALL SCENARIOS PASSED\n'
printf '════════════════════════════════════════════════════════════════════════════\n\n'
