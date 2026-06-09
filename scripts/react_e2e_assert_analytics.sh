#!/usr/bin/env bash
set -euo pipefail

echo "Asserting react-native CLI analytics log: $RN_CLI_LOG"

# --- Invocation IDs ---

if ! grep -q "React Native invocation ID:" "$RN_CLI_LOG"; then
  echo "React Native invocation ID not found in CLI log ❌"
  exit 1
fi
echo "React Native invocation ID present ✅"

# --- Inline parent→child lineage ---
# Children (gradle/xcode/ccache) are no longer reported via a separate per-child
# relation API call. The react-native wrapper enumerates them from the local
# child-stats ledger and reports them inline on its own invocation, logging one
# "Reporting child invocation ... (build tool: X)" line per child.

assert_child_reported() {
  local tool="$1"
  if ! grep -q "Reporting child invocation .* (build tool: ${tool})" "$RN_CLI_LOG"; then
    echo "${tool} child invocation not reported inline on the react-native invocation ❌"
    exit 1
  fi
  echo "${tool} child invocation reported inline ✅"
}

# --- Xcode child (activation detected via xcelerate log files) ---
# The xcodebuild wrapper's output is captured by react-native build-ios and
# doesn't reach $RN_CLI_LOG; its activation is detected via xcelerate-*.log.
# The inline-reporting line itself is emitted by the RN wrapper into $RN_CLI_LOG.

XCELERATE_LOGS=$(find "${BITRISE_DEPLOY_DIR:-.}" -name 'xcelerate-*.log' 2>/dev/null || true)
if [ -n "$XCELERATE_LOGS" ]; then
  echo "Found xcelerate log(s): $XCELERATE_LOGS"
  assert_child_reported "xcode"
else
  echo "No xcelerate log files found (xcode not activated, skipping xcode child check) ℹ️"
fi

# --- Ccache child ---

if grep -q "Ccache invocation ID:" "$RN_CLI_LOG"; then
  echo "Ccache invocation ID present ✅"

  if ! grep -q "Parent invocation ID:" "$RN_CLI_LOG"; then
    echo "Parent invocation ID not found despite ccache being active ❌"
    exit 1
  fi
  echo "Parent invocation ID present ✅"

  assert_child_reported "ccache"
else
  echo "Ccache invocation ID not present (ccache not active or no activity) ℹ️"
fi

# --- HTTP responses (only when debug logging is active) ---

if grep -q "HTTP PUT:" "$RN_CLI_LOG"; then
  # PutInvocation (react-native run invocation, carrying the children inline)
  if ! grep -q "HTTP PUT:.*/v1/invocations/" "$RN_CLI_LOG"; then
    echo "No PutInvocation HTTP call found ❌"
    exit 1
  fi
  echo "PutInvocation HTTP call present ✅"

  # We intentionally do NOT assert on per-child relation calls
  # (PUT /v1/invocations/<parent>/children/<child>): xcode/ccache no longer make
  # them, and gradle's register-child-invocation runs in the gradle daemon, not
  # here. Lineage is verified above via the inline "Reporting child invocation"
  # lines instead.

  # All HTTP responses should be 2xx
  if grep -q "Response: [^2]" "$RN_CLI_LOG"; then
    echo "Non-2xx HTTP response detected ❌"
    grep "Response: [^2]" "$RN_CLI_LOG"
    exit 1
  fi
  echo "All analytics HTTP responses 2xx ✅"
else
  echo "Debug logging not active, skipping HTTP assertions ℹ️"
fi

# --- Failure indicators (should be absent) ---

if grep -q "Failed to send run invocation analytics" "$RN_CLI_LOG"; then
  echo "React-native invocation send failed ❌"
  exit 1
fi

# --- Child-stats ledger aggregation ---
# The react-native wrapper aggregates child invocation hit rates at the end of
# its run and reports the mean on its own invocation. The ledger lives under
# ~/.bitrise/cache/invocations/<parent-id>/ and must be cleaned up after.

rn_invocation_id=$(grep -oE "React Native invocation ID: [a-zA-Z0-9-]+" "$RN_CLI_LOG" | head -1 | awk '{print $NF}' || true)

has_child=false
if grep -q "Ccache invocation ID:" "$RN_CLI_LOG"; then
  has_child=true
fi
if [ -n "${XCELERATE_LOGS:-}" ]; then
  has_child=true
fi

if [ "$has_child" = "true" ]; then
  if ! grep -qE "Cache hit rate \(avg of [0-9]+ child invocations\): [0-9]+\.[0-9]+%" "$RN_CLI_LOG"; then
    echo "Child hit rate aggregation log line missing ❌"
    exit 1
  fi
  echo "Child hit rate aggregation log line present ✅"

  if grep -q "Failed to aggregate child invocation hit rates" "$RN_CLI_LOG"; then
    echo "Aggregation reported an error ❌"
    exit 1
  fi

  if grep -q "Failed to write child stats ledger" "$RN_CLI_LOG"; then
    echo "Ledger writer reported an error ❌"
    exit 1
  fi
else
  echo "No ccache/xcode child detected, skipping aggregation log check ℹ️"
fi

if [ -n "$rn_invocation_id" ] && [ -d "$HOME/.bitrise/cache/invocations/$rn_invocation_id" ]; then
  echo "Ledger dir for RN wrapper was not cleaned up ❌"
  ls -la "$HOME/.bitrise/cache/invocations/$rn_invocation_id" || true
  exit 1
fi
echo "Ledger dir cleaned up after RN run ✅"

echo "All analytics assertions passed ✅"
