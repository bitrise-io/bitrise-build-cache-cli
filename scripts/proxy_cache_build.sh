#!/usr/bin/env bash
#
# Builds through the proxy activate started, with every compile key busted so
# the cache runs cold and every operation is an upload — the path that
# regressed in v3.6.3.
#
#   ./scripts/proxy_cache_build.sh <output-dir>
#
# Runs as its own step so the xcodebuild shim activate published through envman
# is on PATH.

set -uo pipefail

OUT=${1:-}

if [[ -z "$OUT" ]]; then
    echo "usage: $(basename "$0") <output-dir>" >&2
    exit 2
fi

mkdir -p "$OUT"

echo "xcodebuild in use: $(type -p xcodebuild)"

xcodebuild build \
    -workspace ComposableArchitecture.xcworkspace \
    -scheme ComposableArchitecture \
    -destination "generic/platform=iOS" \
    -skipMacroValidation \
    "OTHER_SWIFT_FLAGS=\$(inherited) -DPROXY_E2E_${BITRISE_BUILD_NUMBER:-local}" \
    2>"$OUT/wrapper.log" | tee "$OUT/xcodebuild.log" >/dev/null
echo "${PIPESTATUS[0]}" >"$OUT/xcodebuild.exit"

# The invocation log's directory has moved before; search rather than assume.
find "$HOME/.local/state" -name 'xcelerate*.log' -exec cp {} "$OUT/" \; 2>/dev/null

echo "xcodebuild exit=$(cat "$OUT/xcodebuild.exit")"
echo "xcelerate logs collected: $(find "$OUT" -maxdepth 1 -name 'xcelerate*' | wc -l | tr -d ' ')"
