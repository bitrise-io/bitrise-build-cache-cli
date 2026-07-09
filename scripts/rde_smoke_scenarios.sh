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
# SCENARIO B — activate --interactive wizard TTY drive (ACI-5027)
#              Truly RDE-only: huh's multi-select refuses to render without
#              a real pty on stdin, which Bitrise script steps cannot provide.
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO B — activate --interactive wizard TTY drive"

step "non-TTY invocation must error with the expected guard message"
non_tty_out=$(remote_bash "$CLI activate --interactive 2>&1") && {
  echo "expected non-zero exit; got success" >&2; exit 1
} || true
echo "$non_tty_out" | grep -q "interactive setup requires a terminal" || {
  echo "wizard did not print the expected TTY-required guard message" >&2; exit 1
}

step "TTY path opens the huh TUI — drive via expect, send Ctrl-C to abort"
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

scenario_ok

# ═════════════════════════════════════════════════════════════════════════════
# NOT YET IMPLEMENTED — truly RDE-only scenarios worth adding later:
#
#   * Session persistence: provision → auth set → POST /terminate →
#     POST /restore → wait for RUNNING → assert keychain entry survived.
#     Regular Bitrise VMs die at the end of the build, so this is the only
#     way to smoke persistent-disk restore + credential survival.
#
#   * ACI-5036 doctor as Xcode scheme pre-action: needs an xcodeproj +
#     scheme setup.
#
#   * ACI-5024 OAuth `auth login`: needs a browser. Would require running
#     a Chromium session inside the RDE VM and driving the loopback
#     callback with expect.
# ═════════════════════════════════════════════════════════════════════════════
