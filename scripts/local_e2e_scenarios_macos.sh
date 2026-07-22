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

printf '\n\033[32mmacOS daemon e2e scenarios passed.\033[0m\n'
