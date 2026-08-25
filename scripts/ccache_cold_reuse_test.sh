#!/bin/bash
# Asserts that a ccache entry written by one build is readable by the NEXT build on a
# fresh VM. Run as two phases of a pipeline: PHASE=write, then PHASE=read.
#
# ccache_e2e_test.sh cannot cover this — both of its compiles run in one build, so the
# second reads back what the first just uploaded and any per-build key rotation passes.
set -euo pipefail

: "${PHASE:?PHASE must be 'write' or 'read'}"
CLI="${CCACHE_E2E_CLI:-bitrise-build-cache}"

case "$PHASE" in
write)
  TOKEN="${CCACHE_E2E_TOKEN:-$(cat /proc/sys/kernel/random/uuid)}"
  # Exported for the share-pipeline-variable step that hands them to the read phase.
  envman add --key CCACHE_E2E_TOKEN --value "$TOKEN"
  envman add --key CCACHE_E2E_DC --value "${BITRISE_DEN_VM_DATACENTER:-unknown}"
  ;;
read)
  TOKEN="${CCACHE_E2E_TOKEN:?read phase needs the token written by the write phase}"
  ;;
*)
  echo "Unknown PHASE '$PHASE' ❌"
  exit 1
  ;;
esac

echo "Phase: $PHASE, cache-key token: $TOKEN"

# Both phases must produce a byte-identical compilation. The source lives at a fixed path
# under CCACHE_BASEDIR so its path cannot leak into the key.
SRC_DIR="${BITRISE_SOURCE_DIR:-$PWD}/.ccache-cold-reuse"
SRC="$SRC_DIR/cold_reuse.c"
mkdir -p "$SRC_DIR"
printf 'int add_%s(int a, int b) { return a + b; }\n' "$(echo "$TOKEN" | tr -c 'A-Za-z0-9' '_')" > "$SRC"

COMPILER="$(readlink -f "$(command -v gcc)")"
echo "Compiler: $COMPILER"
stat -c 'Compiler identity: size=%s mtime=%Y (%y)' "$COMPILER"
gcc --version | head -1
DC="${BITRISE_DEN_VM_DATACENTER:-unknown}"
echo "Datacenter: $DC, node: $(hostname)"

# The cache origin is per-datacenter: a read in another DC misses by design and says
# nothing about key stability, which is what this test is for.
if [ "$PHASE" = read ] && [ "$DC" != "${CCACHE_E2E_DC:-$DC}" ]; then
  echo "Write phase ran in ${CCACHE_E2E_DC}, this VM is in ${DC} — cross-DC read, key stability not testable here."
  echo "Skipped ⏭️"
  exit 0
fi

INVOCATION_ID="$(cat /proc/sys/kernel/random/uuid)"
STORAGE_LOG="$HOME/.local/state/ccache/logs/ccache-${INVOCATION_ID}.log"
"$CLI" --debug ccache storage-helper start --invocation-id="$INVOCATION_ID" &
HELPER_PID=$!

cleanup() {
  "$CLI" ccache storage-helper stop 2>/dev/null || true
  wait "$HELPER_PID" 2>/dev/null || true
}
trap cleanup EXIT

for i in $(seq 1 20); do
  if "$CLI" ccache storage-helper set-invocation-id --parent-id="$INVOCATION_ID" --child-id="$INVOCATION_ID" 2>/dev/null; then
    break
  fi
  if [ "$i" -eq 20 ]; then
    echo "Storage helper failed to become ready ❌"
    exit 1
  fi
  sleep 0.5
done

ccache gcc -c "$SRC" -o "$SRC_DIR/cold_reuse.o"

STATS="$(ccache --print-stats --format=json)"
echo "$STATS" | jq '{cache_miss, direct_cache_hit, preprocessed_cache_hit, remote_storage_read_hit, remote_storage_read_miss, remote_storage_write, remote_storage_error}'
counter() { echo "$STATS" | jq ".$1 // 0"; }

fail=0
case "$PHASE" in
write)
  if [ "$(counter remote_storage_write)" -lt 1 ]; then
    echo "Write phase uploaded nothing — nothing for the next build to reuse ❌"
    fail=1
  fi
  ;;
read)
  if [ "$(counter remote_storage_read_hit)" -lt 1 ]; then
    echo "Read phase missed the entry the previous build uploaded ❌"
    echo "The cache key is not stable across builds — compare the compiler identity in both phases."
    fail=1
  fi
  # A write here means this build recreated the entry rather than reusing it.
  if [ "$(counter remote_storage_write)" -gt 0 ] || [ "$(counter cache_miss)" -gt 0 ]; then
    echo "Read phase recompiled and re-uploaded instead of reusing ❌"
    fail=1
  fi
  ;;
esac

"$CLI" ccache storage-helper collect-stats --invocation-id="$INVOCATION_ID"
cleanup
trap - EXIT

if [ "$fail" -ne 0 ]; then
  echo "--- storage helper log ---"
  cat "$STORAGE_LOG" 2>/dev/null || true
  exit 1
fi

echo "Phase $PHASE passed ✅"
