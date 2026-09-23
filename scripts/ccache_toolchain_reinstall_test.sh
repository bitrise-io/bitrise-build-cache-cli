#!/bin/bash
# ccache must reuse an entry across a toolchain reinstall. sdkmanager stamps the NDK with the
# install time, so without CCACHE_COMPILERCHECK=content every key rotates and a real build
# uploads everything and reads back nothing.
set -euo pipefail

SDK="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-/opt/android-sdk-linux}}"
NDK_VERSION="${CCACHE_E2E_NDK_VERSION:-27.1.12297006}"
CLANG="$SDK/ndk/$NDK_VERSION/toolchains/llvm/prebuilt/linux-x86_64/bin/clang"
SDKMANAGER="$SDK/cmdline-tools/latest/bin/sdkmanager"
CLI="${CCACHE_E2E_CLI:-bitrise-build-cache}"

install_ndk() {
  rm -rf "$SDK/ndk/$NDK_VERSION"
  set +o pipefail
  yes | "$SDKMANAGER" --install "ndk;$NDK_VERSION" > /dev/null
  set -o pipefail
  [ -e "$CLANG" ] || { echo "NDK install failed ❌"; exit 1; }
  stat -c "compiler: %n size=%s mtime=%Y" "$(readlink -f "$CLANG")"
}

INVOCATION_ID="$(cat /proc/sys/kernel/random/uuid)"
"$CLI" --debug ccache storage-helper start --invocation-id="$INVOCATION_ID" &
HELPER_PID=$!
trap '"$CLI" ccache storage-helper stop 2>/dev/null || true; wait "$HELPER_PID" 2>/dev/null || true' EXIT

for i in $(seq 1 20); do
  "$CLI" ccache storage-helper set-invocation-id --parent-id="$INVOCATION_ID" --child-id="$INVOCATION_ID" 2>/dev/null && break
  [ "$i" -eq 20 ] && { echo "Storage helper failed to become ready ❌"; exit 1; }
  sleep 0.5
done

SRC="${BITRISE_SOURCE_DIR:-$PWD}/.ccache-reinstall/probe.c"
mkdir -p "$(dirname "$SRC")"
printf 'int add_%s(int a, int b) { return a + b; }\n' "$(cat /proc/sys/kernel/random/uuid | tr -c 'A-Za-z0-9' '_')" > "$SRC"

echo "--- build 1: populate the cache ---"
install_ndk
ccache "$CLANG" -c "$SRC" -o "${SRC%.c}.o"

echo "--- build 2: same source, toolchain reinstalled as the next build would ---"
ccache -z > /dev/null
install_ndk
ccache "$CLANG" -c "$SRC" -o "${SRC%.c}.o"

STATS="$(ccache --print-stats --format=json)"
echo "$STATS" | jq '{cache_miss, direct_cache_hit, preprocessed_cache_hit, remote_storage_read_hit, remote_storage_read_miss, remote_storage_write}'

hit=$(echo "$STATS" | jq '.remote_storage_read_hit // 0')
miss=$(echo "$STATS" | jq '.cache_miss // 0')
if [ "$hit" -lt 1 ] || [ "$miss" -gt 0 ]; then
  echo "The reinstalled toolchain rotated the cache key — build 2 recompiled instead of reusing ❌"
  echo "compiler_check is $(ccache --get-config compiler_check)"
  exit 1
fi

echo "Cache survived the toolchain reinstall ✅"
