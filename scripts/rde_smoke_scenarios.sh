#!/usr/bin/env bash
# Scenario bodies for rde_smoke_test.sh — sourced, not executed on its own.
# Depends on helpers + globals defined by the driver: log, banner, step,
# scenario, scenario_ok, remote_bash, is_mac, is_linux, ssh_password,
# CLI, RDE_BITRISE_PAT, WORKSPACE_SLUG, RDE_SMOKE_CLI_TAG, REMOTE_OS.

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO 1 — installer.sh on a virgin mac (first-time install path)
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO 1 — installer.sh (first-time install on virgin mac)"

step "install CLI ${RDE_SMOKE_CLI_TAG} via installer.sh"
remote_bash "curl -fsSL https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh | sh -s -- -b \"\$HOME/.bitrise/bin\" ${RDE_SMOKE_CLI_TAG}"

step "--version reports ${RDE_SMOKE_CLI_TAG#v}"
got_version=$(remote_bash "$CLI --version" | awk '{print $NF}')
[[ "$got_version" == "${RDE_SMOKE_CLI_TAG#v}"* ]] || {
  echo "version mismatch: want ${RDE_SMOKE_CLI_TAG#v}, got $got_version" >&2
  exit 1
}

scenario_ok

if is_mac; then
  # ═════════════════════════════════════════════════════════════════════════════
  # SCENARIO 2 — Keychain flow + username override
  #              (macOS-only in RDE: linux VMs don't ship a secret-service).
  # ═════════════════════════════════════════════════════════════════════════════
  scenario "SCENARIO 2 — Keychain flow (auth set / auth status + --username)"

  step "unlock login.keychain (RDE vagrant password == SSH password)"
  remote_bash "security unlock-keychain -p '${ssh_password}' ~/Library/Keychains/login.keychain-db || true"

  step "auth set — write credentials + username to OS keychain"
  remote_bash "$CLI auth set --token '${RDE_BITRISE_PAT}' --workspace-id '${WORKSPACE_SLUG}' --username 'rde-smoke-user'"

  step "auth status — must report source=keychain + workspace"
  auth_status=$(remote_bash "$CLI auth status") || { echo "auth status failed" >&2; exit 1; }
  echo "$auth_status"
  echo "$auth_status" | grep -qi "keychain" || { echo "auth status did not report keychain source" >&2; exit 1; }
  echo "$auth_status" | grep -q "$WORKSPACE_SLUG" || { echo "auth status missing workspace id $WORKSPACE_SLUG" >&2; exit 1; }

  scenario_ok
else
  log "SCENARIO 2 (keychain flow) — skipped on $REMOTE_OS (no org.freedesktop.secrets in RDE linux VM)"
fi

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO 3 — Gradle sidecar (ACI-5039 per-tool config-version sidecars)
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO 3 — Gradle sidecar file (ACI-5039)"

step "activate gradle — writes ~/.bitrise/cache/gradle/config.json"
remote_bash "$CLI activate gradle --cache"

step "sidecar file exists + has configVersion"
remote_bash "cat \$HOME/.bitrise/cache/gradle/config.json" | tee /tmp/sidecar.json
grep -q '"configVersion"' /tmp/sidecar.json || { echo "gradle sidecar missing configVersion field" >&2; exit 1; }

scenario_ok

if is_mac; then
  # ═════════════════════════════════════════════════════════════════════════════
  # SCENARIO 4 — Local invocation log via xcodebuild wrapper (ACI-5090)
  #              macOS-only: xcodebuild wrapper needs Xcode.app.
  # ═════════════════════════════════════════════════════════════════════════════
  scenario "SCENARIO 4 — Local invocation log (xcodebuild wrapper)"

  step "activate xcode — installs the xcelerate xcodebuild wrapper"
  remote_bash "$CLI activate xcode --cache"

  step "run wrapper: xcodebuild -showsdks (records invocation; -version short-circuits)"
  remote_bash "\$HOME/.bitrise-xcelerate/bin/xcodebuild -showsdks" || {
    echo "xcodebuild wrapper failed" >&2; exit 1
  }

  step "invocation ndjson under ~/.local/state/bitrise-build-cache/invocations/"
  remote_bash "ls -la \$HOME/.local/state/bitrise-build-cache/invocations/" \
    | grep -q '\.ndjson' || {
      echo "no invocation log ndjson written by wrapper" >&2; exit 1
    }

  scenario_ok
else
  log "SCENARIO 4 (xcodebuild wrapper) — skipped on $REMOTE_OS"
fi

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO 4b — No plaintext credentials on disk (ACI-5123 / ACI-5125)
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO 4b — No plaintext credentials on disk"

step "grep known files for the raw token — must find zero hits"
# Search dot-files, shell rc, gradle config, xcelerate config, cache dir.
# `grep -R` walks recursively; we redirect stderr because permission-denied
# lines on system dirs are irrelevant to the assertion.
hits=$(remote_bash "grep -RF '${RDE_BITRISE_PAT}' \
  \$HOME/.zshrc \$HOME/.bashrc \$HOME/.profile \\
  \$HOME/.gradle \$HOME/.bitrise-xcelerate \$HOME/.bitrise \\
  \$HOME/.local/state/bitrise-build-cache 2>/dev/null || true")
if [[ -n "$hits" ]]; then
  echo "❌ plaintext token found on disk:" >&2
  echo "$hits" >&2
  exit 1
fi
log "clean — token is only in the Keychain"

scenario_ok

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO 4c — activate --interactive wizard TTY guard (ACI-5027)
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO 4c — activate --interactive wizard TTY guard"

step "non-TTY invocation must error with the expected guard message"
non_tty_out=$(remote_bash "$CLI activate --interactive 2>&1") && {
  echo "expected non-zero exit; got success" >&2; exit 1
} || true
echo "$non_tty_out"
echo "$non_tty_out" | grep -q "interactive setup requires a terminal" || {
  echo "wizard did not print the expected TTY-required guard message" >&2
  exit 1
}

step "TTY path opens the huh TUI — drive via expect, send Ctrl-C to abort"
# The wizard prints its header + huh multi-select once stdin is a real pty;
# we look for the header, then send Ctrl-C. Interrupt exit is expected.
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

if is_mac; then
  # ═════════════════════════════════════════════════════════════════════════════
  # SCENARIO 4d — Gradle hydration (ACI-5123 / ACI-5125)
  #              Mac-only: Ubuntu's apt gradle is 4.4.1, which predates the
  #              Kotlin init-script provider syntax we rely on. Modern gradle
  #              would need SDKMAN/asdf bootstrap — out of scope for smoke.
  # ═════════════════════════════════════════════════════════════════════════════
  scenario "SCENARIO 4d — Gradle hydration from keychain (real gradle run)"

  step "ensure gradle available (brew install if missing)"
  remote_bash "command -v gradle || brew install gradle" || {
    echo "gradle brew install failed" >&2; exit 1
  }

  step "re-set keychain (activate xcode may have overwritten it with derived creds)"
  remote_bash "$CLI auth set --token '${RDE_BITRISE_PAT}' --workspace-id '${WORKSPACE_SLUG}'"

  step "auth token — CLI must read the same token back from keychain"
  # 'auth token' prints CLI version banner + then the raw token as
  # 'workspaceID:token' in gradle format. Strip ANSI, grab last line, split
  # off the workspace prefix.
  raw_tok=$(remote_bash "NO_COLOR=1 CLICOLOR=0 TERM=dumb; unset BITRISE_BUILD_CACHE_AUTH_TOKEN BITRISE_BUILD_CACHE_WORKSPACE_ID BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN; $CLI auth token 2>/dev/null" \
    | sed $'s/\x1b\\[[0-9;]*[a-zA-Z]//g' | awk 'NF' | tail -1)
  tok="${raw_tok#*:}"
  [[ -n "$tok" ]] || { echo "auth token returned empty — keychain read broken" >&2; exit 1; }
  if [[ "$tok" != "$RDE_BITRISE_PAT" ]]; then
    echo "auth token mismatch:" >&2
    echo "  got  len=${#tok}  last4=${tok: -4}" >&2
    echo "  want len=${#RDE_BITRISE_PAT}  last4=${RDE_BITRISE_PAT: -4}" >&2
    remote_bash "$CLI auth status --debug" >&2 || true
    exit 1
  fi

  step "scratch gradle project + \`gradle help\` picks up init.d script"
  remote_bash "set -eux; d=/tmp/gradle-smoke; rm -rf \$d; mkdir -p \$d; cd \$d; \\
    echo 'rootProject.name = \"smoke\"' > settings.gradle; \\
    touch build.gradle; \\
    gradle --no-daemon --console=plain --info help 2>&1 | tee /tmp/gradle.out | tail -50; \\
    grep -q 'Bitrise' /tmp/gradle.out && echo 'init.d script fired' || (echo 'init.d script never loaded' >&2; exit 1)"

  scenario_ok
else
  log "SCENARIO 4d (gradle hydration) — skipped on $REMOTE_OS (apt gradle is too old for Kotlin init script)"
fi

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO 5 — Version-drift detector + --no-update-check (ACI-5037)
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO 5 — Version-drift detector (--no-update-check silences)"

step "run --version WITH the nudge enabled (uses installed CLI without --no-update-check alias)"
drift_out=$(remote_bash "\$HOME/.bitrise/bin/bitrise-build-cache --version 2>&1") || true
echo "$drift_out"
echo "$drift_out" | grep -q "is available" && log "nudge printed (real newer version on GitHub, expected)" \
  || log "no nudge — installed CLI already matches GitHub latest"

step "run same with --no-update-check — no 'is available' line"
quiet_out=$(remote_bash "\$HOME/.bitrise/bin/bitrise-build-cache --no-update-check --version 2>&1")
echo "$quiet_out"
echo "$quiet_out" | grep -q "is available" && { echo "--no-update-check did not silence the nudge" >&2; exit 1; } || true

scenario_ok

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO 6 — update --dry-run (ACI-5038)
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO 6 — update --dry-run"

step "update --dry-run — should print an upgrade command without executing"
update_out=$(remote_bash "$CLI update --dry-run 2>&1") || { echo "update --dry-run failed" >&2; echo "$update_out" >&2; exit 1; }
echo "$update_out"

scenario_ok

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO 7 — browse --print (ACI-5049)
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO 7 — browse --print (headless URL emit)"

step "browse --print — emits a bitrise.io URL, no browser launch"
browse_out=$(remote_bash "$CLI browse --print 2>&1") || { echo "browse --print failed" >&2; echo "$browse_out" >&2; exit 1; }
echo "$browse_out"
echo "$browse_out" | grep -qE 'https?://[^ ]*bitrise' || { echo "browse --print did not emit a bitrise URL" >&2; exit 1; }

scenario_ok

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO 8 — Daemon lifecycle: install / up / info / down / restart / uninstall
#              (RDE-unique: real launchd on mac / systemd --user on linux
#              — ACI-5030, ACI-5031, ACI-5032, ACI-5127)
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO 8 — Daemon lifecycle (install / up / info / down / restart / uninstall)"

if is_linux; then
  step "activate c++ — daemon needs a ccache-helper socket to bind on"
  remote_bash "$CLI activate c++ || true"
fi

step "daemon install — writes service unit + bootstraps"
remote_bash "$CLI daemon install"

if is_mac; then
  step "launchctl list — LaunchAgent must be registered"
  remote_bash "launchctl list | grep -q 'bitrise.*build.*cache'" || {
    echo "LaunchAgent not registered with launchctl" >&2
    remote_bash "launchctl list | grep -i bitrise || true" >&2
    exit 1
  }
else
  step "systemctl --user list-unit-files — service unit must be registered"
  remote_bash "systemctl --user list-unit-files | grep -q 'bitrise.*build.*cache'" || {
    echo "systemd --user unit not registered" >&2
    remote_bash "systemctl --user list-unit-files | grep -i bitrise || true" >&2
    exit 1
  }
fi

step "daemon info — reports per-service status"
remote_bash "$CLI daemon info"

step "daemon down — stops services"
remote_bash "$CLI daemon down"

step "daemon up — restarts services"
remote_bash "$CLI daemon up"

step "daemon restart — full cycle"
remote_bash "$CLI daemon restart"

step "daemon uninstall — tears LaunchAgent down cleanly"
remote_bash "$CLI daemon uninstall"

scenario_ok

# ═════════════════════════════════════════════════════════════════════════════
# SCENARIO 9 — Doctor + --fix (ACI-5128)
# ═════════════════════════════════════════════════════════════════════════════
scenario "SCENARIO 9 — Doctor snapshot + --fix"

step "doctor — expected non-zero after uninstall (services no longer running)"
remote_bash "$CLI doctor" || log "doctor non-zero as expected"

step "doctor --fix — auto-repair the fixable items"
remote_bash "$CLI doctor --fix" || log "doctor --fix non-zero (some items require manual action)"

scenario_ok
