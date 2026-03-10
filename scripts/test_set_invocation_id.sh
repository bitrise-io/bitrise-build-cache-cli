#!/usr/bin/env bash
# Test script for `ccache storage-helper set-invocation-id`.
#
# Usage:
#   ./scripts/test_set_invocation_id.sh [socket_path]
#
# The script:
#   1. Builds the CLI binary
#   2. Starts the storage helper in the background (with a mock socket)
#   3. Sends a set-invocation-id request via the CLI command
#   4. Sends a second request with a different ID to verify the logger switches
#   5. Stops the helper and reports results
#
# Requirements:
#   - Go toolchain in PATH
#   - Valid ccache config at ~/.bitrise/cache/ccache/config.json
#     OR provide a socket path and ensure a storage helper is already running
#
# Exit codes: 0 = pass, non-zero = fail

set -euo pipefail

BINARY="./bitrise-build-cache"
SOCKET="${1:-/tmp/test-ccache-ipc.sock}"
INVOCATION_ID_1="aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
INVOCATION_ID_2="11111111-2222-3333-4444-555555555555"

cleanup() {
  if [[ -n "${HELPER_PID:-}" ]]; then
    echo "→ Stopping storage helper (pid $HELPER_PID)"
    kill "$HELPER_PID" 2>/dev/null || true
    wait "$HELPER_PID" 2>/dev/null || true
  fi
  rm -f "$SOCKET"
}
trap cleanup EXIT

echo "=== Build ==="
go build -o "$BINARY" . || { echo "FAIL: build failed"; exit 1; }
echo "OK"

echo ""
echo "=== Start storage helper ==="
"$BINARY" ccache storage-helper start \
  --invocation-id "$INVOCATION_ID_1" \
  &
HELPER_PID=$!
echo "Storage helper started (pid $HELPER_PID)"

# Wait for the socket to appear (up to 5s)
for i in $(seq 1 10); do
  if [[ -S "$SOCKET" ]]; then break; fi
  sleep 0.5
done

if [[ ! -S "$SOCKET" ]]; then
  echo "FAIL: socket $SOCKET did not appear within 5s"
  exit 1
fi
echo "Socket ready: $SOCKET"

echo ""
echo "=== Send first invocation ID ==="
"$BINARY" ccache storage-helper set-invocation-id \
  --id "$INVOCATION_ID_1" \
  --socket "$SOCKET" \
  && echo "OK: first set-invocation-id succeeded" \
  || { echo "FAIL: first set-invocation-id failed"; exit 1; }

echo ""
echo "=== Send second (different) invocation ID ==="
"$BINARY" ccache storage-helper set-invocation-id \
  --id "$INVOCATION_ID_2" \
  --socket "$SOCKET" \
  && echo "OK: second set-invocation-id succeeded" \
  || { echo "FAIL: second set-invocation-id failed"; exit 1; }

echo ""
echo "=== All tests passed ==="
