#!/usr/bin/env bash
# Scenario bodies for rde_smoke_test.sh — sourced, not executed on its own.
#
# Only scenarios that genuinely need a real dev-env VM (fresh state, real
# user keychain / launchd / systemd --user, real TTY) live here. Anything
# that can run under a plain bitrise script step is intentionally covered
# elsewhere in the pipeline — the RDE run must earn its cost.
#
# Depends on helpers + globals defined by the driver: log, banner, step,
# scenario, scenario_ok, remote_bash, parse_ssh_addr, is_mac, is_linux,
# ssh_password, CLI, RDE_BITRISE_PAT, WORKSPACE_SLUG, RDE_SMOKE_CLI_TAG,
# REMOTE_OS.

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO A — Full local-dev journey on ONE fresh VM
#              install → auth → activate → xcodebuild wrapper (mac) →
#              gradle hydration (mac) → on-demand services → doctor
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO A — Full local-dev journey (one fresh VM)"

step "installer.sh install of $RDE_SMOKE_CLI_TAG"
remote_bash "curl -fsSL https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh | sh -s -- -b \"\$HOME/.bitrise/bin\" ${RDE_SMOKE_CLI_TAG}"

# When BRANCH_BINARY_PATH is set (PR CI), scp the branch-built binary
# over the installer output so subsequent scenarios exercise this PR's code.
if [[ -n "${BRANCH_BINARY_PATH:-}" && -f "$BRANCH_BINARY_PATH" ]]; then
  step "overwrite installed CLI with branch binary at $BRANCH_BINARY_PATH"
  SSHPASS="$ssh_password" sshpass -e scp -P "$ssh_port" \
    -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR \
    "$BRANCH_BINARY_PATH" "${ssh_userhost}:/tmp/bbc-branch"
  remote_bash "install -m 0755 /tmp/bbc-branch \$HOME/.bitrise/bin/bitrise-build-cache"
fi

step "--version reports ${RDE_SMOKE_CLI_TAG#v} (or branch build)"
got_version=$(remote_bash "$CLI --version" | awk '{print $NF}')
if [[ -z "${BRANCH_BINARY_PATH:-}" ]]; then
  [[ "$got_version" == "${RDE_SMOKE_CLI_TAG#v}"* ]] || {
    echo "version mismatch: want ${RDE_SMOKE_CLI_TAG#v}, got $got_version" >&2; exit 1
  }
else
  log "installed CLI reports: $got_version (branch override active)"
fi

if is_mac; then
  step "unlock login.keychain (RDE vagrant password == SSH password)"
  remote_bash "security unlock-keychain -p '${ssh_password}' ~/Library/Keychains/login.keychain-db || true"

  step "auth set → keychain (with --username for ACI-4264)"
  remote_bash "$CLI auth set --token '${RDE_BITRISE_PAT}' --workspace-id '${WORKSPACE_SLUG}' --username 'rde-smoke-user'"

  step "auth status must resolve source=keychain + workspace"
  auth_status=$(remote_bash "$CLI auth status")
  echo "$auth_status" | grep -qi "keychain" || { echo "auth status did not report keychain source" >&2; exit 1; }
  echo "$auth_status" | grep -q "$WORKSPACE_SLUG"  || { echo "auth status missing workspace id" >&2; exit 1; }
fi

step "activate gradle → init script + sidecar"
remote_bash "$CLI activate gradle --cache"
remote_bash "cat \$HOME/.bitrise/cache/gradle/config.json" | tee /tmp/sidecar.json
grep -q '"configVersion"' /tmp/sidecar.json || { echo "gradle sidecar missing configVersion field" >&2; exit 1; }

if is_mac; then
  step "activate xcode → xcelerate wrapper installed"
  remote_bash "$CLI activate xcode --cache"

  step "xcodebuild -showsdks via wrapper writes an invocation ndjson (ACI-5090)"
  remote_bash "\$HOME/.bitrise-xcelerate/bin/xcodebuild -showsdks"
  # compgen -G returns non-zero on empty match, so no need for pipefail games.
  remote_bash "compgen -G \"\$HOME/.local/state/bitrise-build-cache/invocations/*.ndjson\" >/dev/null" || {
    echo "no invocation log ndjson written by wrapper" >&2
    remote_bash "ls -la \$HOME/.local/state/bitrise-build-cache/invocations/ 2>/dev/null || echo '(dir missing)'" >&2
    exit 1
  }
fi

if is_mac; then
  step "no plaintext credentials on disk after every activation (keychain-backed)"
  # macOS-only: on linux we DELIBERATELY put the token in ~/.bitrise/env
  # (0600) because the RDE VM has no secret-service, so the assertion is
  # only meaningful on mac.
  hits=$(remote_bash "grep -RF '${RDE_BITRISE_PAT}' \
    \$HOME/.zshrc \$HOME/.bashrc \$HOME/.profile \\
    \$HOME/.gradle \$HOME/.bitrise-xcelerate \$HOME/.bitrise \\
    \$HOME/.local/state/bitrise-build-cache 2>/dev/null || true")
  if [[ -n "$hits" ]]; then
    echo "❌ plaintext token found on disk:" >&2; echo "$hits" >&2; exit 1
  fi
fi

if is_mac; then
  step "gradle hydration end-to-end (auth token → BitriseAuthTokenSource)"
  remote_bash "command -v gradle || brew install gradle"
  # activate xcode may overwrite the keychain with derived creds; reset before we compare.
  remote_bash "$CLI auth set --token '${RDE_BITRISE_PAT}' --workspace-id '${WORKSPACE_SLUG}'"
  raw_tok=$(remote_bash "NO_COLOR=1 CLICOLOR=0 TERM=dumb; unset BITRISE_BUILD_CACHE_AUTH_TOKEN BITRISE_BUILD_CACHE_WORKSPACE_ID BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN; $CLI auth token 2>/dev/null" \
    | sed $'s/\x1b\\[[0-9;]*[a-zA-Z]//g' | awk 'NF' | tail -1)
  # auth token returns workspaceID:token gradle-format for PATs — strip the prefix.
  tok="${raw_tok#*:}"
  [[ "$tok" == "$RDE_BITRISE_PAT" ]] || { echo "auth token mismatch (last4 got=${tok: -4} want=${RDE_BITRISE_PAT: -4})" >&2; exit 1; }
  # Anchored to the init-script's own analytics log tags. These come from
  # the plugin code path, so they only appear if the init.d/*.kts actually
  # loaded (not from any random Bitrise output).
  remote_bash "set -eux; d=/tmp/gradle-smoke; rm -rf \$d; mkdir -p \$d; cd \$d; \\
    echo 'rootProject.name = \"smoke\"' > settings.gradle; \\
    touch build.gradle; \\
    gradle --no-daemon --console=plain --info help 2>&1 | tee /tmp/gradle.out | tail -50; \\
    grep -qE '\\[Bitrise Analytics\\]|\\[Bitrise Build Session\\]' /tmp/gradle.out"
fi

step "activate c++ — must leave a storage helper serving"
remote_bash "$CLI activate c++"
remote_bash 'test -e "${TMPDIR:-/tmp}/ccache-ipc.sock"' || {
  echo "activate c++ left no storage helper serving" >&2; exit 1
}

step "no supervisor unit may be registered"
if is_mac; then
  remote_bash "ls ~/Library/LaunchAgents/io.bitrise.build-cache.*.plist >/dev/null 2>&1" && {
    echo "activation registered a launch agent" >&2; exit 1
  } || true
else
  remote_bash "systemctl --user list-unit-files | grep -q 'bitrise.*build.*cache'" && {
    echo "activation registered a systemd unit" >&2; exit 1
  } || true
fi

step "doctor snapshot + --fix (smoke: binary runs, exit codes tolerated)"
remote_bash "$CLI doctor" || log "doctor non-zero as expected on a partially-configured VM"
remote_bash "$CLI doctor --fix" || log "doctor --fix non-zero (some items require manual action)"

scenario_ok

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO B — activate --interactive wizard (ACI-5027)
#              Three paths:
#                (1) non-TTY without TERM=dumb must guard-error.
#                (2) expect over ssh -tt: verifies the huh TUI actually
#                    renders under a real pty and can be exited cleanly.
#                (3) TERM=dumb accessible mode: line-based Q&A on stdin,
#                    exercises the wizard's submit flow end-to-end without
#                    a pty.
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO B — activate --interactive wizard (guard + TUI render + accessible drive)"

step "non-TTY invocation without TERM=dumb must error with the guard message"
non_tty_out=$(remote_bash "$CLI activate --interactive 2>&1") && {
  echo "expected non-zero exit; got success" >&2; exit 1
} || true
echo "$non_tty_out" | grep -q "interactive setup requires a terminal" || {
  echo "wizard did not print the expected TTY-required guard message" >&2; exit 1
}

step "TTY path renders the huh TUI — drive via expect, send Ctrl-C to abort"
# Ctrl-C in expect returns via eof (exit 0); a real render-timeout exits 2.
# We WANT to fail on timeout, so no swallowing '|| true' here.
remote_bash "cat > /tmp/wizard.exp <<'WEXP'
set timeout 20
spawn env NO_COLOR=1 [file join \$env(HOME) .bitrise/bin/bitrise-build-cache] activate --interactive
expect {
  -re \"interactive local setup\" { send -- \"\x03\"; exp_continue }
  eof { exit 0 }
  timeout { puts stderr \"wizard did not render its header within 20s\"; exit 2 }
}
WEXP
expect -f /tmp/wizard.exp"

step "TERM=dumb drives the huh accessible mode (line-based Q&A on stdin)"
# huh auto-switches to accessible mode when TERM=dumb. With keychain seeded
# by SCENARIO A the wizard prompts: tools multi-select → username → push
# confirm. All tools preselected; pipe: 0 (confirm the default) → '' (keep
# username) → n (no push).
# 'export' (not 'TERM=dumb <cmd>' prefix) so TERM=dumb survives the
# `. ~/.bitrise/env` source that $CLI performs on Linux.
remote_bash "export TERM=dumb; $CLI activate --interactive <<'EOF'
0

n
EOF"

scenario_ok

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO C — Session persistence across terminate → restore
#              Truly RDE-only: only RDE sessions have a persistent disk
#              that survives a stop/start cycle.
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO C — Session persistence across terminate → restore"

if is_mac; then
  step "seed keychain marker with the current tag before terminate"
  marker="rde-smoke-persist-${RDE_SMOKE_CLI_TAG}"
  remote_bash "security unlock-keychain -p '${ssh_password}' ~/Library/Keychains/login.keychain-db || true"
  remote_bash "$CLI auth set --token '${RDE_BITRISE_PAT}' --workspace-id '${WORKSPACE_SLUG}' --username '${marker}'"

  step "POST /terminate — VM stops, disk stays"
  curl_rde POST "${WS_PATH}/sessions/${session_id}/terminate" -d '{}' >/dev/null
  for _ in $(seq 1 24); do
    st=$(curl_rde GET "${WS_PATH}/sessions/${session_id}" | jq -r '.session.status // empty')
    [[ "$st" == "SESSION_STATUS_TERMINATED" ]] && break
    sleep 5
  done
  [[ "$st" == "SESSION_STATUS_TERMINATED" ]] || {
    echo "session did not reach TERMINATED (last: $st)" >&2; exit 1
  }
  log "terminated"

  step "POST /restore — VM is re-created from the persistent disk"
  curl_rde POST "${WS_PATH}/sessions/${session_id}/restore" -d '{}' >/dev/null
  new_addr="" new_pw=""
  for i in $(seq 1 60); do
    s=$(curl_rde GET "${WS_PATH}/sessions/${session_id}")
    st=$(echo "$s" | jq -r '.session.status // empty')
    open=$(echo "$s" | jq -r '.session.sshConnectionOpen // false')
    if [[ "$st" == "SESSION_STATUS_RUNNING" && "$open" == "true" ]]; then
      new_addr=$(echo "$s" | jq -r '.session.sshAddress // empty')
      new_pw=$(echo "$s"   | jq -r '.session.sshPassword // empty')
      break
    fi

    sleep 10
  done
  [[ "$st" == "SESSION_STATUS_RUNNING" ]] || {
    echo "session did not restore to RUNNING (last: $st)" >&2; exit 1
  }
  log "restored + sshd back"

  # Rebind ssh globals via the shared parse helper.
  parse_ssh_addr "$new_addr"
  ssh_password="$new_pw"
  SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=10 -p "$ssh_port")

  step "keychain marker must survive the restore"
  remote_bash "security unlock-keychain -p '${ssh_password}' ~/Library/Keychains/login.keychain-db || true"
  status_after=$(remote_bash "$CLI auth status")
  echo "$status_after"
  echo "$status_after" | grep -q "$marker" || {
    echo "keychain marker '$marker' not found after restore" >&2; exit 1
  }

  step "xcelerate config survived the restore"
  remote_bash "test -f \$HOME/.bitrise-xcelerate/config.json"

  step "xcodebuild wrapper still records an invocation after restore"
  ndjson_before=$(remote_bash "cat \$HOME/.local/state/bitrise-build-cache/invocations/*.ndjson 2>/dev/null | wc -l | tr -d ' '")
  remote_bash "\$HOME/.bitrise-xcelerate/bin/xcodebuild -showsdks >/dev/null"
  ndjson_after=$(remote_bash "cat \$HOME/.local/state/bitrise-build-cache/invocations/*.ndjson 2>/dev/null | wc -l | tr -d ' '")
  log "invocation ndjson line count: before=$ndjson_before after=$ndjson_after"
  [[ "$ndjson_after" -gt "$ndjson_before" ]] || {
    echo "wrapper did not append a fresh invocation after restore" >&2; exit 1
  }

  scenario_ok
else
  log "SCENARIO C (session persistence) — skipped on $REMOTE_OS (linux VM has no user keychain to persist)"
fi

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO D — auth login callback-URL paste fallback (ACI-5241)
#              The reason the paste path exists: an RDE session's browser runs
#              on the user's laptop, not the VM, so it can never reach the CLI's
#              127.0.0.1 callback listener. Needs a real pty (the fallback only
#              arms when stdin is a terminal), so this is RDE/expect-only.
#
#              No IdP round-trip: the authorize URL the CLI prints carries the
#              state + redirect_uri, which is everything needed to drive the
#              paste parser and its rejection paths.
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO D — auth login accepts a pasted callback URL"

step "login prints the paste hint and rejects a mismatched state"
# expect drives a pty so the paste fallback is armed. We paste a callback URL built
# from the printed authorize URL but with a deliberately wrong state — proving
# the paste was read and validated without needing a real authorization code.
remote_bash "cat > /tmp/paste.exp <<'PEXP'
set timeout 30
log_user 1
spawn env NO_COLOR=1 [file join \$env(HOME) .bitrise/bin/bitrise-build-cache] auth login --workspace ${WORKSPACE_SLUG}
set redirect \"\"
expect {
  -re {redirect_uri=http%3A%2F%2F127.0.0.1%3A([0-9]+)%2Fcallback} {
    set port \$expect_out(1,string)
    set redirect \"http://127.0.0.1:\$port/callback\"
    exp_continue
  }
  -re {copy the URL from the browser} {
    if {\$redirect eq \"\"} { puts stderr \"hint printed before the authorize URL\"; exit 3 }
    send -- \"\$redirect?code=dummy-code&state=WRONG-STATE\r\"
    exp_continue
  }
  -re {state mismatch} { exit 0 }
  eof { puts stderr \"login exited without reporting a state mismatch\"; exit 4 }
  timeout { puts stderr \"login did not print the paste hint within 30s\"; exit 2 }
}
PEXP
expect -f /tmp/paste.exp"

step "an unusable paste is nudged instead of aborting the login"
remote_bash "cat > /tmp/paste_junk.exp <<'PEXP'
set timeout 30
log_user 1
spawn env NO_COLOR=1 [file join \$env(HOME) .bitrise/bin/bitrise-build-cache] auth login --workspace ${WORKSPACE_SLUG}
expect {
  -re {copy the URL from the browser} { send -- \"https://app.bitrise.io/dashboard\r\"; exp_continue }
  -re {doesn't look like the callback URL} { exit 0 }
  eof { puts stderr \"login exited instead of nudging on an unusable paste\"; exit 4 }
  timeout { puts stderr \"no nudge for the unusable paste within 30s\"; exit 2 }
}
PEXP
expect -f /tmp/paste_junk.exp"

scenario_ok

# ═════════════════════════════════════════════════════════════════════════════
# NOT YET IMPLEMENTED — RDE-only scenarios worth adding later:
#
#   * ACI-5036 doctor as Xcode scheme pre-action: needs an xcodeproj +
#     scheme setup on the RDE mac.
# ═════════════════════════════════════════════════════════════════════════════
