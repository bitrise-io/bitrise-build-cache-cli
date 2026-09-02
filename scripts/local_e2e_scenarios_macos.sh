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

log "activate xcode + c++ supervises nothing and leaves the ccache helper serving"
"$CLI" activate xcode --cache-push=false >/dev/null
"$CLI" activate c++ --cache-push=false >/dev/null

# Nothing is supervised: a launchd job lands in its own resource coalition and
# competes with the compiler it serves, so every service is started by whoever
# needs it. See docs/daemon-latency.md.
ls ~/Library/LaunchAgents/io.bitrise.build-cache.*.plist >/dev/null 2>&1 \
  && fail "activation wrote a launch agent; nothing may be supervised"
pass "activation registered no supervised service"

# activate c++ must leave a helper serving, or ccache silently misses every
# lookup for the whole build.
[ -e "$TMPDIR/ccache-ipc.sock" ] || fail "activate c++ left no ccache storage helper serving"
pass "activate c++ started the ccache storage helper"

# A CLI at or below v3.6.9 may have left a launch agent behind. It would keep
# restarting a supervised — and therefore slow — helper, so activation retires it.
log "activation removes a launch agent left by an older CLI"
mkdir -p ~/Library/LaunchAgents
cat >~/Library/LaunchAgents/io.bitrise.build-cache.ccache-helper.plist <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>io.bitrise.build-cache.ccache-helper</string>
  <key>ProgramArguments</key><array><string>/usr/bin/true</string></array>
</dict></plist>
PLIST
"$CLI" activate c++ --cache-push=false >/dev/null
[ ! -f ~/Library/LaunchAgents/io.bitrise.build-cache.ccache-helper.plist ] \
  || fail "activation left a stale ccache-helper launch agent in place"
pass "activation removed the stale launch agent"

# The wizard is the path most local users take, and it used to be the only one
# that started the helper, so assert it too.
log "activate --interactive supervises nothing and starts the helper"
FAKE_TOKEN=${FAKE_TOKEN:-bitpat_fake-token-for-ci-e2e}
FAKE_WS=${FAKE_WS:-fake-workspace-id}
# Seeded so the wizard resolves credentials from the keychain and never reaches
# the browser sign-in path.
"$CLI" auth set --token "$FAKE_TOKEN" --workspace-id "$FAKE_WS" >/dev/null
rm -f "$TMPDIR"/ccache-ipc.sock 2>/dev/null || true
pkill -f 'ccache storage-helper' 2>/dev/null || true

# Accessible-mode answers: 0 = confirm the default (all four tools preselected),
# '' = keep display name, n = no cache push. Multi-toggle across huh
# accessible-mode redraws is not portable across huh versions, so the scenario
# accepts the default selection.
TERM=dumb "$CLI" activate --interactive <<'EOF'
0

n
EOF

ls ~/Library/LaunchAgents/io.bitrise.build-cache.*.plist >/dev/null 2>&1 \
  && fail "the wizard wrote a launch agent; nothing may be supervised"
[ -e "$TMPDIR/ccache-ipc.sock" ] || fail "the wizard left no ccache storage helper serving"
pass "wizard activated without supervising, and started the helper"

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

printf '\n\033[32mmacOS e2e scenarios passed.\033[0m\n'
