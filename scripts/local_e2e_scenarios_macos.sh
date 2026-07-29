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
log "activate --interactive selecting Xcode registers the proxy with launchd"
FAKE_TOKEN=${FAKE_TOKEN:-bitpat_fake-token-for-ci-e2e}
FAKE_WS=${FAKE_WS:-fake-workspace-id}
# Seeded so the wizard resolves credentials from the keychain and never reaches
# the browser sign-in path.
"$CLI" auth set --token "$FAKE_TOKEN" --workspace-id "$FAKE_WS" >/dev/null

# Accessible-mode answers: 3 = toggle Xcode (1-indexed: Gradle/Bazel/Xcode/ccache),
# 0 = confirm selection, '' = keep display name, n = no cache push,
# y = yes to keeping the proxies running.
TERM=dumb "$CLI" activate --interactive <<'EOF'
3
0

n
y
EOF

[ -f ~/Library/LaunchAgents/io.bitrise.build-cache.xcelerate-proxy.plist ] \
  || fail "wizard did not write the xcelerate-proxy plist"
launchctl list | grep -q io.bitrise.build-cache.xcelerate-proxy \
  || fail "wizard did not register xcelerate-proxy with launchd"
# Xcode alone must not drag in the ccache helper.
[ ! -f ~/Library/LaunchAgents/io.bitrise.build-cache.ccache-helper.plist ] \
  || fail "wizard registered ccache-helper for an Xcode-only selection"
pass "wizard installed + started only the services Xcode needs"

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

# A cache backend that accepts connections and never answers. Verified to make
# xcodebuild emit CAS errors, which no setup check can see — the setup is fine,
# the backend isn't. grpc:// (not grpcs://) keeps it TLS-free.
log "end-of-build report names compilation-cache errors"
BH_PORT=59777
BH_DIR=$(mktemp -d)
cat > "$BH_DIR/hole.go" <<'GOEOF'
package main

import (
	"net"
	"os"
)

func main() {
	l, err := net.Listen("tcp", os.Args[1])
	if err != nil {
		panic(err)
	}
	// Accept and hold: the peer waits for a reply that never comes, rather than
	// seeing a connection reset.
	var held []net.Conn
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		held = append(held, c)
	}
}
GOEOF
# Refuse to run against a port someone else holds: a stale listener would serve
# the build and the assertions would pass without this scenario having done
# anything.
nc -z 127.0.0.1 "$BH_PORT" 2>/dev/null \
  && fail "port $BH_PORT is already in use — refusing to test against someone else's listener"

# Built rather than `go run`: go run execs a child, so $! would be the wrapper
# and the teardown would orphan the listener holding the port.
go build -o "$BH_DIR/hole" "$BH_DIR/hole.go" || fail "could not build the black-hole backend"
"$BH_DIR/hole" "127.0.0.1:$BH_PORT" &
BH_PID=$!
disown "$BH_PID" 2>/dev/null || true
for _ in $(seq 1 30); do
  nc -z 127.0.0.1 "$BH_PORT" 2>/dev/null && break
  sleep 0.5
done
nc -z 127.0.0.1 "$BH_PORT" 2>/dev/null \
  || fail "black-hole backend never bound $BH_PORT (pid $BH_PID)"

# A one-file package is enough to drive real compilation, and so real cache
# lookups. The auto-generated scheme is "<name>-Package", and a package with no
# platforms declared yields no usable scheme at all.
mkdir -p "$BH_DIR/pkg/Sources/Tiny"
cat > "$BH_DIR/pkg/Package.swift" <<'PKGEOF'
// swift-tools-version:5.9
import PackageDescription
let package = Package(
    name: "Tiny",
    platforms: [.macOS(.v13)],
    targets: [.target(name: "Tiny")]
)
PKGEOF
printf 'public struct Tiny { public init() {}; public func f() -> Int { 41 + 1 } }\n' \
  > "$BH_DIR/pkg/Sources/Tiny/Tiny.swift"

"$CLI" xcelerate stop-proxy >/dev/null 2>&1 || true
"$CLI" activate xcode --cache --cache-push=false \
  --cache-endpoint "grpc://127.0.0.1:$BH_PORT" >/dev/null

# Watchdog: every cache lookup waits out a deadline against the black hole, so
# this build is slow by construction. macOS has no timeout(1), and an unbounded
# hang here would burn the whole workflow slot.
( cd "$BH_DIR/pkg" && ~/.bitrise-xcelerate/bin/xcodebuild build \
  -scheme Tiny-Package -destination "platform=macOS" \
  -derivedDataPath "$BH_DIR/dd" > "$BH_DIR/build.log" 2>&1 ) &
BUILD_PID=$!
( sleep 300; kill -TERM "$BUILD_PID" 2>/dev/null ) & WATCHDOG_PID=$!
disown "$WATCHDOG_PID" 2>/dev/null || true
wait "$BUILD_PID" 2>/dev/null || true
kill "$WATCHDOG_PID" 2>/dev/null || true
BH_OUT=$(cat "$BH_DIR/build.log" 2>/dev/null || true)

kill "$BH_PID" 2>/dev/null || true
for _ in $(seq 1 10); do
  nc -z 127.0.0.1 "$BH_PORT" 2>/dev/null || break
  sleep 0.5
done
nc -z 127.0.0.1 "$BH_PORT" 2>/dev/null \
  && fail "black-hole backend outlived the scenario on port $BH_PORT"
"$CLI" xcelerate stop-proxy >/dev/null 2>&1 || true
"$CLI" activate xcode --cache --cache-push=false >/dev/null
rm -rf "$BH_DIR"

echo "$BH_OUT" | grep -q "CAS error" \
  || { echo "$BH_OUT" | tail -30; fail "unreachable backend produced no CAS errors — the probe setup is wrong"; }
echo "$BH_OUT" | grep -qE "compilation cache reported [0-9]+ error" \
  || { echo "$BH_OUT" | tail -30; fail "end-of-build report did not name the compilation-cache errors"; }
pass "compilation-cache errors surfaced in the end-of-build report"

printf '\n\033[32mmacOS daemon e2e scenarios passed.\033[0m\n'
