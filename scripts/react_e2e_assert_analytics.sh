#!/usr/bin/env bash
set -euo pipefail

echo "Asserting react-native CLI analytics log: $RN_CLI_LOG"

# --- Invocation IDs ---

if ! grep -q "React Native invocation ID:" "$RN_CLI_LOG"; then
  echo "React Native invocation ID not found in CLI log ❌"
  exit 1
fi
echo "React Native invocation ID present ✅"

# --- Xcode parent-child invocation relation (checked via xcelerate log files) ---
# The xcodebuild wrapper's output is captured by react-native build-ios and doesn't
# reach $RN_CLI_LOG. Instead, TInfof messages are written to xcelerate log files
# at $BITRISE_DEPLOY_DIR/xcelerate-*.log.

XCELERATE_LOGS=$(find "${BITRISE_DEPLOY_DIR:-.}" -name 'xcelerate-*.log' 2>/dev/null || true)
if [ -n "$XCELERATE_LOGS" ]; then
  echo "Found xcelerate log(s): $XCELERATE_LOGS"

  if ! grep -q "Registering invocation relation:.*build-tool=xcode" $XCELERATE_LOGS; then
    echo "Xcode invocation relation not registered ❌"
    exit 1
  fi
  echo "Xcode invocation relation registered ✅"

  if grep -q "Failed to send invocation relation analytics" $XCELERATE_LOGS; then
    echo "Xcode invocation relation send failed ❌"
    exit 1
  fi
  echo "Xcode invocation relation send succeeded ✅"
else
  echo "No xcelerate log files found (xcode not activated, skipping xcode relation checks) ℹ️"
fi

# --- Ccache invocation relation ---

if grep -q "Ccache invocation ID:" "$RN_CLI_LOG"; then
  echo "Ccache invocation ID present ✅"

  if ! grep -q "Parent invocation ID:" "$RN_CLI_LOG"; then
    echo "Parent invocation ID not found despite ccache being active ❌"
    exit 1
  fi
  echo "Parent invocation ID present ✅"
else
  echo "Ccache invocation ID not present (ccache not active or no activity) ℹ️"
  if grep -q "HTTP PUT:.*/v1/invocations/.*/children/" "$RN_CLI_LOG"; then
    echo "Unexpected ccache invocation relation HTTP call found when ccache was inactive ❌"
    exit 1
  fi
  echo "No unexpected ccache relation HTTP calls ✅"
fi

# --- HTTP responses (only when debug logging is active) ---

if grep -q "HTTP PUT:" "$RN_CLI_LOG"; then
  # PutInvocation (react-native run invocation)
  if ! grep -q "HTTP PUT:.*/v1/invocations/" "$RN_CLI_LOG"; then
    echo "No PutInvocation HTTP call found ❌"
    exit 1
  fi
  echo "PutInvocation HTTP call present ✅"

  # PutInvocationRelation (parent→ccache) — only when ccache was activated
  if grep -q "Ccache invocation ID:" "$RN_CLI_LOG"; then
    if ! grep -q "HTTP PUT:.*/v1/invocations/.*/children/" "$RN_CLI_LOG"; then
      echo "No PutInvocationRelation HTTP call found ❌"
      exit 1
    fi
    echo "PutInvocationRelation HTTP call present ✅"
  fi

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

if grep -q "Warning: failed to send run invocation analytics" "$RN_CLI_LOG"; then
  echo "React-native invocation send failed ❌"
  exit 1
fi

if grep -q "Warning: failed to register invocation relation" "$RN_CLI_LOG"; then
  echo "Invocation relation registration failed ❌"
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
  if ! grep -qE "Cache hit rate: [0-9]+\.[0-9]+% \(avg of [0-9]+ child invocations\)" "$RN_CLI_LOG"; then
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
