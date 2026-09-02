#!/bin/bash
set -euo pipefail

CLI=${CLI:-bitrise-build-cache}

log()  { printf '\n\033[36m▶ %s\033[0m\n' "$*"; }
fail() { printf '\n\033[31m✗ %s\033[0m\n' "$*" >&2; exit 1; }
pass() { printf '\033[32m  ✓ %s\033[0m\n' "$*"; }

launchctl bootout "gui/$UID/io.bitrise.build-cache.xcelerate-proxy" 2>/dev/null || true
launchctl bootout "gui/$UID/io.bitrise.build-cache.ccache-helper"    2>/dev/null || true
rm -f ~/Library/LaunchAgents/io.bitrise.build-cache.*.plist
rm -f "$TMPDIR"/xcelerate-proxy.sock "$TMPDIR"/ccache-ipc.sock 2>/dev/null || true
rm -rf ~/.local/state/bitrise-build-cache/logs/

log "activate xcode + c++ (prerequisite for daemon install)"
"$CLI" activate xcode --cache-push=false >/dev/null
"$CLI" activate c++ --cache-push=false >/dev/null
pass "xcode + c++ activated"

log "daemon install → plists + launchctl registration"
"$CLI" daemon install
[ -f ~/Library/LaunchAgents/io.bitrise.build-cache.xcelerate-proxy.plist ] || fail "xcelerate-proxy plist missing"
[ -f ~/Library/LaunchAgents/io.bitrise.build-cache.ccache-helper.plist ]   || fail "ccache-helper plist missing"
launchctl list | grep -q io.bitrise.build-cache.xcelerate-proxy || fail "xcelerate-proxy not registered with launchctl"
launchctl list | grep -q io.bitrise.build-cache.ccache-helper   || fail "ccache-helper not registered with launchctl"
pass "plists written + services registered with launchd"

log "daemon info reports per-service status shape"
INFO=$("$CLI" daemon info --json | sed -n '/^{/,/^}/p')
for key in xcelerateProxy xcelerateProxyStatus ccacheHelper ccacheHelperStatus; do
  echo "$INFO" | jq -e ".${key}" >/dev/null || fail "daemon info --json missing ${key}"
done
pass "daemon info --json contract ok"

log "daemon down → xcelerate socket removed, entries deregistered"
"$CLI" daemon down
sleep 1
launchctl list | grep -q io.bitrise.build-cache && fail "launchctl still lists services after daemon down" || true
[ ! -e "$TMPDIR/xcelerate-proxy.sock" ] || fail "xcelerate socket still present after daemon down"
pass "daemon down deregistered + xcelerate socket unlinked"

if [ ! -e "$TMPDIR/ccache-ipc.sock" ]; then
  pass "ccache socket unlinked on daemon down (ACI-5179 landed)"
else
  printf '\033[33m  ⚠ ccache socket persists — expected until ACI-5179 (PR #414) merges\033[0m\n'
  rm -f "$TMPDIR/ccache-ipc.sock"
fi

log "daemon up → re-registers services"
"$CLI" daemon up
sleep 1
launchctl list | grep -q io.bitrise.build-cache.xcelerate-proxy || fail "xcelerate-proxy not re-registered by daemon up"
launchctl list | grep -q io.bitrise.build-cache.ccache-helper   || fail "ccache-helper not re-registered by daemon up"
pass "daemon up re-registered services"

log "daemon uninstall → plists gone, launchctl empty"
"$CLI" daemon uninstall
ls ~/Library/LaunchAgents/io.bitrise.build-cache.*.plist 2>/dev/null | grep -q . && fail "plists remain after uninstall" || true
launchctl list | grep -q io.bitrise.build-cache && fail "launchctl still lists services after uninstall" || true
pass "daemon uninstall ok"

# The wizard is the path most local users take, so assert it reaches launchd —
# not just that `daemon install` does. TERM=dumb drives huh's accessible mode
# from a pipe, so this needs no TTY.
log "activate --interactive registers the daemon plists via the wizard"
FAKE_TOKEN=${FAKE_TOKEN:-bitpat_fake-token-for-ci-e2e}
FAKE_WS=${FAKE_WS:-fake-workspace-id}
# Seeded so the wizard resolves credentials from the keychain and never reaches
# the browser sign-in path.
"$CLI" auth set --token "$FAKE_TOKEN" --workspace-id "$FAKE_WS" >/dev/null

# Accessible-mode answers: 0 = confirm the default (all four tools preselected),
# '' = keep display name, n = no cache push, y = yes to keeping the proxies
# running. Multi-toggle across huh accessible-mode redraws is not portable across
# huh versions, so the scenario accepts the default selection.
TERM=dumb "$CLI" activate --interactive <<'EOF'
0

n
y
EOF

# Nothing is supervised any more: a launchd job lands in its own resource
# coalition and competes with the compiler it serves, so both the proxy and the
# ccache helper are started by whoever needs them. `daemon install` still
# supervises on request. See docs/daemon-latency.md.
[ ! -f ~/Library/LaunchAgents/io.bitrise.build-cache.xcelerate-proxy.plist ] \
  || fail "wizard wrote an xcelerate-proxy plist; activation must not supervise"
[ ! -f ~/Library/LaunchAgents/io.bitrise.build-cache.ccache-helper.plist ] \
  || fail "wizard wrote a ccache-helper plist; activation must not supervise"
pass "wizard activated without registering any supervised service"

# Explicit opt-in still works, and warns.
INSTALL_OUT=$("$CLI" daemon install 2>&1)
echo "$INSTALL_OUT" | grep -q "Supervised services are measurably slower" \
  || fail "daemon install did not warn about supervision"
[ -f ~/Library/LaunchAgents/io.bitrise.build-cache.xcelerate-proxy.plist ] \
  || fail "daemon install did not write the xcelerate-proxy plist"
launchctl list | grep -q io.bitrise.build-cache.xcelerate-proxy \
  || fail "daemon install did not register xcelerate-proxy with launchd"
pass "daemon install still supervises on request, with a warning"

"$CLI" daemon uninstall >/dev/null

# Proves the health check reports real problems, not just that it runs. A build
# action is required — query actions (-list, -showsdks) skip the check by design —
# but the build itself may fail immediately, since the check runs before it.
log "xcodebuild wrapper health check surfaces broken auth"
"$CLI" auth clear >/dev/null 2>&1 || true
GATE_DIR=$(mktemp -d)
# The CI JWT is env-injected and is a legitimate credential, so it has to go too
# or the check correctly reports healthy and proves nothing.
GATE_OUT=$(cd "$GATE_DIR" && env -u BITRISE_BUILD_CACHE_AUTH_TOKEN -u BITRISE_BUILD_CACHE_WORKSPACE_ID \
  -u BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN \
  ~/.bitrise-xcelerate/bin/xcodebuild build -scheme BitriseHealthCheckProbe 2>&1 || true)
rm -rf "$GATE_DIR"

echo "$GATE_OUT" | grep -q "health check found issues" \
  || { echo "$GATE_OUT" | tail -40; fail "wrapper did not report health check issues with auth cleared"; }
# Either the start-of-build auth check or the save-failure backend probe may name
# it, depending on which credential source is left; both are the gate working.
echo "$GATE_OUT" | grep -qE "(auth|auth-backend): " \
  || { echo "$GATE_OUT" | tail -40; fail "health check did not name the auth problem"; }
pass "health check reported the broken auth around the build"

"$CLI" auth set --token "$FAKE_TOKEN" --workspace-id "$FAKE_WS" >/dev/null

printf '\n\033[32mmacOS daemon e2e scenarios passed.\033[0m\n'
