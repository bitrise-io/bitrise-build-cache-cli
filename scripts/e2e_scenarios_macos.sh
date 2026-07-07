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

log "daemon install → both services running (via post-bootstrap kickstart)"
"$CLI" daemon install
sleep 2
INFO=$("$CLI" daemon info --json | sed -n '/^{/,/^}/p')
echo "$INFO" | jq -e '.xcelerateProxyStatus == "running"' >/dev/null || fail "xcelerate-proxy not running: $INFO"
echo "$INFO" | jq -e '.ccacheHelperStatus == "running"'   >/dev/null || fail "ccache-helper not running: $INFO"
pass "daemon install brought both services up"

log "daemon down → xcelerate socket removed, both stopped"
"$CLI" daemon down
sleep 1
[ ! -e "$TMPDIR/xcelerate-proxy.sock" ] || fail "xcelerate socket still present after daemon down"
[ ! -e "$TMPDIR/ccache-ipc.sock" ]      || fail "ccache socket still present after daemon down"
pass "sockets unlinked on daemon down"

log "daemon up → services running again"
"$CLI" daemon up
sleep 2
INFO=$("$CLI" daemon info --json | sed -n '/^{/,/^}/p')
echo "$INFO" | jq -e '.xcelerateProxyStatus == "running"' >/dev/null || fail "xcelerate-proxy did not come back up"
echo "$INFO" | jq -e '.ccacheHelperStatus == "running"'   >/dev/null || fail "ccache-helper did not come back up"
pass "daemon up ok"

log "daemon uninstall → plists gone, launchctl empty"
"$CLI" daemon uninstall
ls ~/Library/LaunchAgents/io.bitrise.build-cache.*.plist 2>/dev/null | grep -q . && fail "plists remain after uninstall" || true
launchctl list | grep -q io.bitrise.build-cache && fail "launchctl still shows service" || true
pass "daemon uninstall ok"

printf '\n\033[32mmacOS daemon e2e scenarios passed.\033[0m\n'
