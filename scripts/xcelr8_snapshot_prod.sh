#!/usr/bin/env bash
# Xcelr8 hackathon-only.
# Pulls a snapshot of the user's prod invocations from bitrise-website's
# BuildCache::InvocationsController#index for each tool, into
# `_xcelr8/snapshots/`. Used to validate the ACI-4914 presenter contract
# (Go `InvocationSummary` struct) against real wire data, and to seed
# follow-up local-replay experiments.
#
# Reads workspace + PAT from `~/.bitrise/cache/ccache/config.json`.
# Output dir is gitignored — files contain real PII (project IDs, commit
# hashes, repo URLs, build slugs). Do NOT commit.
#
# Usage:
#   bash scripts/xcelr8_snapshot_prod.sh [items_per_page]
#
# Default items_per_page=50. Window is the controller default (last 30
# days).

set -euo pipefail

CONFIG="${BITRISE_CCACHE_CONFIG:-$HOME/.bitrise/cache/ccache/config.json}"
ITEMS="${1:-50}"
OUT_DIR="$(git rev-parse --show-toplevel)/_xcelr8/snapshots"

if [[ ! -f "$CONFIG" ]]; then
  echo "missing $CONFIG" >&2
  exit 1
fi

PAT="$(jq -r .authConfig.AuthToken "$CONFIG")"
WS="$(jq -r .authConfig.WorkspaceID "$CONFIG")"

if [[ -z "$PAT" || -z "$WS" || "$PAT" == "null" || "$WS" == "null" ]]; then
  echo "could not read PAT / WorkspaceID from $CONFIG" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
echo "ws=$WS pat=${PAT:0:10}... → $OUT_DIR"

for tool in xcode gradle bazel react-native ccache; do
  url="https://app.bitrise.io/build-cache/$WS/invocations.json?tool=$tool&items_per_page=$ITEMS"
  out="$OUT_DIR/list-$tool.json"

  printf "  %-13s " "$tool"
  http_code="$(curl -sS -o "$out" -w "%{http_code}" "$url" -H "Authorization: token $PAT")"

  if [[ "$http_code" != "200" ]]; then
    printf "HTTP %s — skipping\n" "$http_code"
    rm -f "$out"
    continue
  fi

  count="$(jq '.items | length' "$out")"
  total="$(jq '.paging.totalCount' "$out")"
  printf "items=%s totalCount=%s\n" "$count" "$total"
done

echo "done."
