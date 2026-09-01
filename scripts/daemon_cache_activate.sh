#!/usr/bin/env bash
#
# Activates the cache and starts the launchd-supervised xcelerate proxy.
#
#   ./scripts/daemon_cache_activate.sh <output-dir>
#
# Must run as its own step: activate publishes its xcodebuild shim through
# envman, which only takes effect in the next step. Building in this one would
# use the unwrapped xcodebuild and the cache would never engage.
#
# activate deliberately skips the daemon on CI, so the guard installs it by
# hand: customers do not get the launchd proxy, but this still exercises it.

set -euo pipefail

OUT=${1:-}

if [[ -z "$OUT" ]]; then
    echo "usage: $(basename "$0") <output-dir>" >&2
    exit 2
fi

CLI="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/bitrise-build-cache-cli"
mkdir -p "$OUT"

# Start from a clean proxy and a clean log directory, or a stale proxy serves
# the build and the log counts include a previous run.
"$CLI" daemon down 2>/dev/null || true
"$CLI" daemon uninstall 2>/dev/null || true
pkill -f 'xcelerate start-proxy' 2>/dev/null || true
rm -rf "$HOME/.local/state/xcelerate/logs"
sleep 2

# A local fake backend keeps a PR gate off the shared backend: no credentials,
# no cross-DC variance, and no probe blobs written into a real workspace.
#
# Verified to still catch the regression rather than merely being cheaper: with
# the proxy flipped to ProcessType Background it recorded 38 timeouts against 0
# on Interactive. Loopback is fast enough that this was in doubt — the real
# backend produced 128 — so re-run that control before trusting any change here.
ENDPOINT_ARGS=""
if [[ "${DAEMON_E2E_FAKE_BACKEND:-0}" == "1" ]]; then
    REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    go test -tags probe -c -o "$OUT/fake.test" "$REPO/internal/xcelerate/proxy/"

    # Everything misses, so every compile uploads — the cold-build path.
    FAKE_BACKEND_SERVE=1 FAKE_BACKEND_HIT_RATE=0.0 \
        FAKE_BACKEND_ENDPOINT_FILE="$OUT/endpoint.txt" \
        nohup "$OUT/fake.test" -test.run TestFakeBackendServe -test.v \
        >"$OUT/fake-backend.log" 2>&1 &
    disown

    for _ in $(seq 1 30); do
        [[ -s "$OUT/endpoint.txt" ]] && break
        sleep 1
    done
    ENDPOINT="$(tr -d '\n' <"$OUT/endpoint.txt" 2>/dev/null)"
    if [[ -z "$ENDPOINT" ]]; then
        echo "FAIL: the fake backend never reported an endpoint" >&2
        tail -20 "$OUT/fake-backend.log" >&2
        exit 1
    fi
    echo "fake backend: $ENDPOINT"
    ENDPOINT_ARGS="--cache-endpoint $ENDPOINT"
fi

# shellcheck disable=SC2086 # ENDPOINT_ARGS is empty or two words, deliberately unquoted
"$CLI" -d activate xcode --cache $ENDPOINT_ARGS 2>&1 | tee "$OUT/activate.log"
"$CLI" daemon install 2>&1 | tee -a "$OUT/activate.log"
"$CLI" daemon up 2>&1 | tee -a "$OUT/activate.log"

launchctl print "gui/$(id -u)/io.bitrise.build-cache.xcelerate-proxy" >"$OUT/launchctl.txt" 2>&1

# Recorded so the assert can prove the build went through *this* proxy rather
# than one the wrapper forked for itself.
DAEMON_PID="$(awk '/pid = /{print $3; exit}' "$OUT/launchctl.txt")"
if [[ -z "$DAEMON_PID" ]]; then
    echo "FAIL: no launchd xcelerate proxy after daemon up" >&2
    cat "$OUT/launchctl.txt" >&2
    exit 1
fi

echo "$DAEMON_PID" >"$OUT/daemon.pid"
echo "launchd xcelerate proxy pid: $DAEMON_PID"
