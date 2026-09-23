#!/usr/bin/env bash
#
# Prints cache-operation latency percentiles from xcelerate logs.
#
#   ./scripts/xcelerate_op_latency.sh <dir-with-xcelerate-logs>
#
# Reads the "... took <duration>" line the proxy writes per operation and
# reports count/p50/p90/p99 in milliseconds, as: OPLAT n=… p50=… p90=… p99=…
#
# Diagnostics only. Against the local fake backend these percentiles do not
# distinguish a throttled proxy from a healthy one — they come out backwards,
# because loopback latency is dominated by the build, not the proxy. The timeout
# count is what gates. Numbers in docs/daemon-latency.md.
#
# Sorting happens outside awk because macOS awk has no asort.

set -uo pipefail

DIR=${1:-}

if [[ -z "$DIR" ]]; then
    echo "usage: $(basename "$0") <dir-with-xcelerate-logs>" >&2
    exit 2
fi

# Only the analytics payload line is dropped -- it dumps the whole environment,
# commit message included, so a commit mentioning DeadlineExceeded would fail its
# own build. Matching the whole "[Bitrise Analytics]" prefix would be wrong: the
# wrapper logs the proxy-attach line under it too.
find "$DIR" -maxdepth 1 \( -name '*.log' -o -name 'xcelerate*' \) -print0 2>/dev/null |
    xargs -0 cat 2>/dev/null |
    grep -av 'Payload: {' |
    grep -av '"envs"' |
    grep -aoE 'took [0-9.]+(ns|µs|us|ms|s)' |
    awk '{
        v = $2 + 0
        unit = $2
        sub(/^[0-9.]+/, "", unit)
        if (unit == "ns") print v / 1000000
        else if (unit == "µs" || unit == "us") print v / 1000
        else if (unit == "ms") print v
        else print v * 1000
    }' |
    sort -n |
    awk '{ a[n++] = $1 }
    END {
        if (n == 0) { print "OPLAT n=0"; exit }
        printf "OPLAT n=%d p50=%.1fms p90=%.1fms p99=%.1fms max=%.1fms\n", \
            n, a[int(n * 0.50)], a[int(n * 0.90)], a[int(n * 0.99)], a[n - 1]
    }'
