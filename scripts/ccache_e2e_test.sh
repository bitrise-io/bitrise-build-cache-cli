#!/bin/bash
set -euxo pipefail

TEST_INVOCATION_ID=$(cat /proc/sys/kernel/random/uuid)
STORAGE_LOG="$HOME/.local/state/ccache/logs/ccache-${TEST_INVOCATION_ID}.log"

# Start the storage helper in the background.
# --debug ensures GET operation lines appear in the per-invocation log file.
# Socket path and auth are read from the config written by activate c++ above.
./bitrise-build-cache-cli --debug ccache storage-helper start --invocation-id="$TEST_INVOCATION_ID" &
HELPER_PID=$!

# Poll until the helper is accepting connections
for i in $(seq 1 20); do
  if ./bitrise-build-cache-cli ccache storage-helper set-invocation-id \
      --parent-id="$TEST_INVOCATION_ID" --child-id="$TEST_INVOCATION_ID" 2>/dev/null; then
    echo "Storage helper ready after $i attempt(s)"
    break
  fi
  if [ "$i" -eq 20 ]; then
    echo "Storage helper failed to become ready ❌"
    kill "$HELPER_PID" 2>/dev/null || true
    exit 1
  fi
  sleep 0.5
done

TEST_DIR=$(mktemp -d)
printf 'int add(int a, int b) { return a + b; }\n' > "$TEST_DIR/test.c"

# Build 1: may be a miss (first ever run) or a hit (subsequent runs) — no assertion.
ccache gcc -c "$TEST_DIR/test.c" -o "$TEST_DIR/test1.o"
echo "Build 1 done"

# Build 2: same source — always a GET hit (either from build 1's PUT or from a
# previous CI run that already stored this key in the remote cache).
ccache gcc -c "$TEST_DIR/test.c" -o "$TEST_DIR/test2.o"
echo "Build 2 done — expected GET hit"

# Assert remote_storage_read_hit > 0 from ccache's own JSON stats (before collect-stats zeros them)
REMOTE_READ_HITS=$(ccache --print-stats --format=json | jq '.remote_storage_read_hit // 0')
echo "remote_storage_read_hit: $REMOTE_READ_HITS"
if [ "$REMOTE_READ_HITS" -lt 1 ]; then
  echo "No remote storage read hits in ccache stats ❌"
  # Stop the helper before exiting
  ./bitrise-build-cache-cli ccache storage-helper stop
  wait "$HELPER_PID" || true
  exit 1
fi
echo "ccache remote_storage_read_hit confirmed ✅"

# Collect and send ccache stats to the analytics backend, then zero the counters
./bitrise-build-cache-cli ccache storage-helper collect-stats --invocation-id="$TEST_INVOCATION_ID"
echo "collect-stats completed ✅"

# Stop the helper gracefully and wait for it to flush and exit
./bitrise-build-cache-cli ccache storage-helper stop
wait "$HELPER_PID" || true

# Build 2 must have been a remote GET hit.
# Log format (Debug): "[Get - <hex-hash>] OK took Xms"  (CI: asserted here)
grep -P '\[Get - [0-9a-f]+\] OK took' "$STORAGE_LOG"
echo "Remote GET (cache hit) confirmed in storage helper log ✅"
