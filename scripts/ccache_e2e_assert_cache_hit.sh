#!/usr/bin/env bash
set -euo pipefail

STATS=$(ccache --show-stats)
echo "$STATS"

# ccache 4.x shows a "Remote storage" section; assert at least one read hit
if ! echo "$STATS" | grep -A10 "Remote storage" | grep -qE "Read hit:[[:space:]]+[1-9]"; then
  echo "No remote cache hits detected ❌"
  exit 1
fi

echo "Remote cache hit confirmed ✅"
