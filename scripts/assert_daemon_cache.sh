#!/usr/bin/env bash
#
# Fails if the launchd-supervised proxy timed out on any cache operation.
#
#   ./scripts/assert_daemon_cache.sh <output-dir>
#
# A healthy proxy serves the whole build without a single DeadlineExceeded, so
# the threshold is zero. The regression put 30-60% of operations there.
#
# The count, not a latency percentile. Against the local fake backend the two
# configurations are indistinguishable by latency and in fact rank backwards —
# measured p90 was 483ms on the healthy Interactive proxy against 393ms on a
# throttled Background one, both topping out near 998ms — while timeouts
# separated cleanly at 0 against 38. Latency percentiles only tell the two apart
# against the real backend (1931ms vs 5637ms), which this deliberately does not
# use. OPLAT is reported for diagnosis; do not gate on it.

set -uo pipefail

DIR=${1:-}

if [[ -z "$DIR" ]]; then
    echo "usage: $(basename "$0") <output-dir>" >&2
    exit 2
fi

# macOS ships bash 3.2, so no mapfile and no readarray here.
#
# The analytics payload is dropped because it dumps the whole environment,
# including the commit message. A commit that so much as mentions
# DeadlineExceeded would otherwise fail its own build.
cat_logs() {
    find "$DIR" -maxdepth 1 \( -name '*.log' -o -name 'xcelerate*' \) -print0 2>/dev/null |
        xargs -0 cat 2>/dev/null |
        grep -av 'Payload: {' |
        grep -av '"envs"'
}

count() {
    cat_logs | grep -acE -- "$1" || true
}

# A build that did not compile, or one whose cache never engaged, says nothing
# about the proxy.
rc=$(cat "$DIR/xcodebuild.exit" 2>/dev/null || echo 1)
if [[ "$rc" != "0" ]]; then
    echo "INCONCLUSIVE: xcodebuild did not succeed (exit $rc)" >&2
    exit 1
fi

# Without this the workflow would still pass if the wrapper forked its own
# proxy, which is the pre-v3.6.3 path and is not what this guards.
daemon_pid=$(cat "$DIR/daemon.pid" 2>/dev/null)
attached_pid=$(cat_logs | grep -aoE 'proxy already running \(pid: [0-9]+' | grep -oE '[0-9]+$' | head -1)

if [[ -z "$daemon_pid" ]]; then
    echo "INCONCLUSIVE: no launchd proxy pid was recorded at activate time" >&2
    exit 1
fi
if [[ "$attached_pid" != "$daemon_pid" ]]; then
    echo "FAIL: the build did not go through the launchd proxy (daemon pid $daemon_pid, wrapper attached to '${attached_pid:-none}')" >&2
    exit 1
fi

echo "built through the launchd proxy (pid $daemon_pid)"

ops=$(count 'Cache (hit|miss)|(Get|Put|Load|Save|GetValue|PutValue) (took|ok)')
if [[ "$ops" -eq 0 ]]; then
    echo "INCONCLUSIVE: cache never engaged (0 operations logged)" >&2
    exit 1
fi

errs=$(count 'DeadlineExceeded')

printf '%s cache operations, %s DeadlineExceeded\n' "$ops" "$errs"
"$(dirname "${BASH_SOURCE[0]}")/xcelerate_op_latency.sh" "$DIR"

if [[ "$errs" -ne 0 ]]; then
    echo "FAIL: the launchd proxy timed out on $errs cache operations" >&2
    cat_logs | grep -aE 'DeadlineExceeded' | head -5 >&2

    exit 1
fi

echo "OK: no cache operation timed out"
