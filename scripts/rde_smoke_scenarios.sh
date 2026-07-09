#!/usr/bin/env bash
# Scenario bodies for rde_smoke_test.sh — sourced, not executed on its own.
#
# Only scenarios that genuinely need a real dev-env VM (fresh state, real
# user keychain / launchd / systemd --user, real TTY) live here. Anything
# that can run under a plain bitrise script step (unit tests, dry-runs,
# grep-only asserts, non-interactive subcommands) is intentionally covered
# elsewhere in the pipeline — the RDE run must earn its cost.
#
# Depends on helpers + globals defined by the driver: log, banner, step,
# scenario, scenario_ok, remote_bash, is_mac, is_linux, ssh_password,
# CLI, RDE_BITRISE_PAT, WORKSPACE_SLUG, RDE_SMOKE_CLI_TAG, REMOTE_OS.

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO A — Full local-dev journey on ONE fresh VM
#              install → auth → activate → xcodebuild (mac) → gradle
#              hydration (mac) → daemon full lifecycle → doctor
#
#              Individually most of these steps are also covered by plain
#              Bitrise workflows; the value of running them here is chaining
#              them on a single first-time-user VM so state carries through
#              the sequence — the same way a real end-user would exercise it.
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO A — Full local-dev journey (one fresh VM)"

step "installer.sh install of $RDE_SMOKE_CLI_TAG"
remote_bash "curl -fsSL https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh | sh -s -- -b \"\$HOME/.bitrise/bin\" ${RDE_SMOKE_CLI_TAG}"

step "--version reports ${RDE_SMOKE_CLI_TAG#v}"
got_version=$(remote_bash "$CLI --version" | awk '{print $NF}')
[[ "$got_version" == "${RDE_SMOKE_CLI_TAG#v}"* ]] || {
  echo "version mismatch: want ${RDE_SMOKE_CLI_TAG#v}, got $got_version" >&2; exit 1
}

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

step "no plaintext credentials on disk (rc files, gradle, xcelerate, state)"
hits=$(remote_bash "grep -RF '${RDE_BITRISE_PAT}' \
  \$HOME/.zshrc \$HOME/.bashrc \$HOME/.profile \\
  \$HOME/.gradle \$HOME/.bitrise-xcelerate \$HOME/.bitrise \\
  \$HOME/.local/state/bitrise-build-cache 2>/dev/null || true")
if [[ -n "$hits" ]]; then
  echo "❌ plaintext token found on disk:" >&2; echo "$hits" >&2; exit 1
fi

if is_mac; then
  step "activate xcode → xcelerate wrapper installed"
  remote_bash "$CLI activate xcode --cache"

  step "xcodebuild -showsdks via wrapper writes an invocation ndjson (ACI-5090)"
  remote_bash "\$HOME/.bitrise-xcelerate/bin/xcodebuild -showsdks"
  remote_bash "ls -la \$HOME/.local/state/bitrise-build-cache/invocations/" \
    | grep -q '\.ndjson' || { echo "no invocation log ndjson written by wrapper" >&2; exit 1; }

  step "gradle hydration end-to-end (auth token → BitriseAuthTokenSource)"
  remote_bash "command -v gradle || brew install gradle"
  # activate xcode may overwrite the keychain with derived creds; reset before we compare.
  remote_bash "$CLI auth set --token '${RDE_BITRISE_PAT}' --workspace-id '${WORKSPACE_SLUG}'"
  raw_tok=$(remote_bash "NO_COLOR=1 CLICOLOR=0 TERM=dumb; unset BITRISE_BUILD_CACHE_AUTH_TOKEN BITRISE_BUILD_CACHE_WORKSPACE_ID BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN; $CLI auth token 2>/dev/null" \
    | sed $'s/\x1b\\[[0-9;]*[a-zA-Z]//g' | awk 'NF' | tail -1)
  # auth token returns workspaceID:token gradle-format for PATs — strip the prefix.
  tok="${raw_tok#*:}"
  [[ "$tok" == "$RDE_BITRISE_PAT" ]] || { echo "auth token mismatch (last4 got=${tok: -4} want=${RDE_BITRISE_PAT: -4})" >&2; exit 1; }
  remote_bash "set -eux; d=/tmp/gradle-smoke; rm -rf \$d; mkdir -p \$d; cd \$d; \\
    echo 'rootProject.name = \"smoke\"' > settings.gradle; \\
    touch build.gradle; \\
    gradle --no-daemon --console=plain --info help 2>&1 | tee /tmp/gradle.out | tail -50; \\
    grep -q 'Bitrise' /tmp/gradle.out"
fi

if is_linux; then
  step "activate c++ — daemon needs a ccache-helper socket to bind on"
  remote_bash "$CLI activate c++ || true"
fi

step "daemon install / list / info / down / up / restart / uninstall"
remote_bash "$CLI daemon install"
if is_mac; then
  remote_bash "launchctl list | grep -q 'bitrise.*build.*cache'" \
    || { echo "LaunchAgent not registered" >&2; exit 1; }
else
  remote_bash "systemctl --user list-unit-files | grep -q 'bitrise.*build.*cache'" \
    || { echo "systemd --user unit not registered" >&2; exit 1; }
fi
remote_bash "$CLI daemon info"
remote_bash "$CLI daemon down"
remote_bash "$CLI daemon up"
remote_bash "$CLI daemon restart"
remote_bash "$CLI daemon uninstall"

step "doctor snapshot + --fix"
remote_bash "$CLI doctor" || log "doctor non-zero as expected on a partially-configured VM"
remote_bash "$CLI doctor --fix" || log "doctor --fix non-zero (some items require manual action)"

scenario_ok

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO B — activate --interactive wizard (ACI-5027)
#              Three paths:
#                (1) non-TTY without --tools must guard-error.
#                (2) TTY via expect: verifies the huh TUI actually renders
#                    (real pty in RDE — regular Bitrise script steps can't).
#                    Sends Ctrl-C to abort so we don't need to script the
#                    full multi-select selection flow.
#                (3) --tools drives runSelectedTools() headlessly, exercising
#                    the underlying flow the huh TUI reaches on submit.
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO B — activate --interactive wizard (guard + TUI render + --tools drive)"

step "non-TTY invocation without --tools must error with the guard message"
non_tty_out=$(remote_bash "$CLI activate --interactive 2>&1") && {
  echo "expected non-zero exit; got success" >&2; exit 1
} || true
echo "$non_tty_out" | grep -q "interactive setup requires a terminal" || {
  echo "wizard did not print the expected TTY-required guard message" >&2; exit 1
}

step "TTY path renders the huh TUI — drive via expect, send Ctrl-C to abort"
remote_bash "cat > /tmp/wizard.exp <<'WEXP'
set timeout 20
spawn env NO_COLOR=1 [file join \$env(HOME) .bitrise/bin/bitrise-build-cache] activate --interactive
expect {
  -re \"interactive local setup\" { send -- \"\x03\"; exp_continue }
  eof { exit 0 }
  timeout { puts stderr \"wizard did not render its header within 20s\"; exit 2 }
}
WEXP
expect -f /tmp/wizard.exp || true # Ctrl-C exit is expected"

step "--tools=gradle drives the wizard headlessly (skips huh, uses env/keychain auth)"
# The --tools flag is only present in this PR and later releases; the installer
# tag resolved by 'git describe' is the last released version, which may
# predate the flag. Detect + skip if the installed CLI doesn't know it yet.
if remote_bash "$CLI activate --interactive --help 2>&1 | grep -q -- --tools"; then
  remote_bash "$CLI activate --interactive --tools=gradle --push=false"
else
  log "--tools not present in installed CLI (${RDE_SMOKE_CLI_TAG}); skipping headless drive"
fi

step "TERM=dumb drives the huh accessible mode (line-based Q&A on stdin)"
# huh auto-switches to accessible mode when TERM=dumb. Assuming keychain
# already carries the token (SCENARIO A seeded it), the wizard prompts
# for: tools multi-select → username → push confirm. Pipe answers:
#   1     → toggle option 1 (Gradle)
#   0     → confirm selection
#   ''    → keep existing username
#   n     → no cache push
# TERM=dumb bypass only works in versions that carry the guard-relax patch
# from this PR — feature-detect the same way we did for --tools.
if remote_bash "$CLI activate --interactive --help 2>&1 | grep -q 'TERM=dumb'"; then
  remote_bash "TERM=dumb $CLI activate --interactive <<'EOF' >/tmp/wizard.out 2>&1
1
0

n
EOF
  cat /tmp/wizard.out"
else
  log "TERM=dumb guard-relax not present in installed CLI; skipping accessible drive"
fi

scenario_ok

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO C — Session persistence across terminate → restore
#              Truly RDE-only: only RDE sessions have a persistent disk
#              that survives a stop/start cycle. Regular Bitrise VMs die
#              at the end of the build.
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
  # Poll status until RUNNING + SSH open again, then re-parse sshAddress.
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

  # Rebind ssh globals for the rest of the scenario (and any subsequent ones).
  ssh_userhost=$(printf '%s\n' "$new_addr" | grep -oE '[[:alnum:]._-]+@[[:alnum:]._-]+' | tail -1)
  new_port=$(printf '%s\n' "$new_addr" | grep -oE '\-p[[:space:]]+[0-9]+' | tail -1 | awk '{print $NF}')
  : "${new_port:=22}"
  ssh_password="$new_pw"
  SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=10 -p "$new_port")

  step "keychain marker must survive the restore"
  remote_bash "security unlock-keychain -p '${ssh_password}' ~/Library/Keychains/login.keychain-db || true"
  status_after=$(remote_bash "$CLI auth status")
  echo "$status_after"
  echo "$status_after" | grep -q "$marker" || {
    echo "keychain marker '$marker' not found after restore" >&2; exit 1
  }

  scenario_ok
else
  log "SCENARIO C (session persistence) — skipped on $REMOTE_OS (linux VM has no user keychain to persist)"
fi

# ═════════════════════════════════════════════════════════════════════════════
# NOT YET IMPLEMENTED — RDE-only scenarios worth adding later:
#
#   * ACI-5036 doctor as Xcode scheme pre-action: needs an xcodeproj +
#     scheme setup on the RDE mac.
# ═════════════════════════════════════════════════════════════════════════════
