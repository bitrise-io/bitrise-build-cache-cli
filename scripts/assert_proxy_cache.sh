#!/usr/bin/env bash
#
# Fails if the proxy timed out on any cache operation.
#
#   ./scripts/assert_proxy_cache.sh <output-dir>
#
# A healthy proxy serves the whole build without a single DeadlineExceeded, so
# the threshold is zero.
#
# Gate on the count, never a latency percentile: against the local fake backend
# the healthy and throttled proxies are indistinguishable by percentile and in
# fact rank backwards, while the timeout count separates them cleanly. OPLAT is
# reported for diagnosis only. Numbers in docs/daemon-latency.md.

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

# The proxy must actually have served this build; a run where it never started
# would report zero timeouts for the wrong reason.
proxy_pid=$(cat_logs | grep -aoE 'Started xcelerate_proxy pid = [0-9]+' | grep -oE '[0-9]+$' | head -1)
if [[ -z "$proxy_pid" ]]; then
    proxy_pid=$(cat_logs | grep -aoE 'proxy already running \(pid: [0-9]+' | grep -oE '[0-9]+$' | head -1)
fi

if [[ -z "$proxy_pid" ]]; then
    echo "INCONCLUSIVE: the wrapper never reported a proxy" >&2
    exit 1
fi

echo "built through the wrapper-forked proxy (pid $proxy_pid)"

ops=$(count 'Cache (hit|miss)|(Get|Put|Load|Save|GetValue|PutValue) (took|ok)')
if [[ "$ops" -eq 0 ]]; then
    echo "INCONCLUSIVE: cache never engaged (0 operations logged)" >&2
    exit 1
fi

# Both spellings: the gRPC code is "DeadlineExceeded", but a caller that gave up
# queueing for a pool slot reports Go's "context deadline exceeded" instead, and
# that one is invisible to the first pattern.
errs=$(count 'DeadlineExceeded|context deadline exceeded')
# Queue starvation and a slow backend both surface as DeadlineExceeded, and the
# caller's timeout covers both, so the split says which one it was.
starved=$(count 'acquire kv channel')

printf '%s cache operations, %s DeadlineExceeded (%s waiting for a pool slot)\n' \
    "$ops" "$errs" "$starved"
"$(dirname "${BASH_SOURCE[0]}")/xcelerate_op_latency.sh" "$DIR"

if [[ "$errs" -ne 0 ]]; then
    echo "FAIL: the launchd proxy timed out on $errs cache operations" >&2
    cat_logs | grep -aE 'DeadlineExceeded' | head -5 >&2

    exit 1
fi

echo "OK: no cache operation timed out"
