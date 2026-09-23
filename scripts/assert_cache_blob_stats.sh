#!/usr/bin/env bash
# Validates the cacheBlobStats payload the wrapper PUT to analytics, read back out of a
# wrapper log written with `activate xcode -d`.
#
# Only invariants are asserted, never absolute counts: op counts and byte totals move with
# cache warmth, so anything thresholded here would flake. Presence is enough to prove
# recording ran, because an empty snapshot is sent as no field at all.
set -uo pipefail

log="${1:?usage: assert_cache_blob_stats.sh <wrapper log>}"
[ -f "$log" ] || { echo "FAIL: no such log: $log"; exit 1; }

# The wrapper writes the payload compact on one line, prefixed by "Payload:" and possibly
# coloured. Take the last one that carries the field: the enrichment re-PUT sends others.
payload=""
while IFS= read -r candidate; do
  if jq -e 'has("cacheBlobStats")' >/dev/null 2>&1 <<<"$candidate"; then
    payload="$candidate"
  fi
done < <(sed $'s/\033\\[[0-9;]*m//g' "$log" | grep -o '{"invocationId".*}')

if [ -z "$payload" ]; then
  echo "FAIL: no analytics payload carrying cacheBlobStats in $log"
  echo "Payload lines seen (field absent means nothing was recorded):"
  sed $'s/\033\\[[0-9;]*m//g' "$log" | grep -c '{"invocationId".*}' || true
  exit 1
fi

failures=$(jq -r '
  .cacheBlobStats as $b
  | [ if $b.schemaVersion != 1 then "schemaVersion is \($b.schemaVersion), expected 1" else empty end,
      ( ["download", "upload"][] as $dir
        | $b[$dir] as $t | $b.cas[$dir] as $cas | $b.kv[$dir] as $kv
        | [ if $cas == null or $kv == null then "\($dir): cas/kv breakdown missing" else empty end,
            if $cas.opCount + $kv.opCount != $t.opCount
              then "\($dir): opCount \($t.opCount) != cas \($cas.opCount) + kv \($kv.opCount)" else empty end,
            if $cas.bytesTotal + $kv.bytesTotal != $t.bytesTotal
              then "\($dir): bytesTotal does not equal cas + kv" else empty end,
            if $cas.missCount + $kv.missCount != $t.missCount
              then "\($dir): missCount does not equal cas + kv" else empty end,
            if $t.opCount != $t.latencyMs.count
              then "\($dir): opCount \($t.opCount) != latencyMs.count \($t.latencyMs.count)" else empty end,
            if $t.opCount != $t.sizeBytes.count
              then "\($dir): opCount \($t.opCount) != sizeBytes.count \($t.sizeBytes.count)" else empty end,
            ( ["latencyMs", "sizeBytes"][] as $h
              | if ($t[$h].counts | add) != $t[$h].count
                  then "\($dir).\($h): bucket counts do not sum to count" else empty end,
                if ($t[$h].counts | length) != (($t[$h].boundaries | length) + 1)
                  then "\($dir).\($h): counts must be one longer than boundaries" else empty end ),
            if $t.throughput.histogram.count + $t.throughput.excludedSmallOps != $t.opCount
              then "\($dir): throughput histogram + excludedSmallOps != opCount" else empty end,
            if $t.throughput.p10BytesPerSec > $t.throughput.p50BytesPerSec
               or $t.throughput.p50BytesPerSec > $t.throughput.p90BytesPerSec
              then "\($dir): throughput percentiles out of order" else empty end
          ][] ) ]
  | .[]' <<<"$payload")

if [ -n "$failures" ]; then
  echo "FAIL: cacheBlobStats is not self-consistent:"
  printf '  %s\n' "$failures"
  jq '.cacheBlobStats' <<<"$payload"
  exit 1
fi

jq -r '.cacheBlobStats as $b
  | "cacheBlobStats OK (schema \($b.schemaVersion)): "
    + ( ["download", "upload"]
        | map( . as $d | "\($d) ops=\($b[$d].opCount) (cas \($b.cas[$d].opCount) + kv \($b.kv[$d].opCount))"
               + " miss=\($b[$d].missCount) err=\($b[$d].errorCount) bytes=\($b[$d].bytesTotal)"
               + " latency p_min=\($b[$d].latencyMs.min)ms p_max=\($b[$d].latencyMs.max)ms"
               + " thr_p50=\($b[$d].throughput.p50BytesPerSec)B/s" )
        | join(" | ") )' <<<"$payload"
