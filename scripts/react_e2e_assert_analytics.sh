#!/usr/bin/env bash
set -euo pipefail

echo "Asserting react-native CLI analytics log: $RN_CLI_LOG"

PLAIN_LOG="$(mktemp)"
trap 'rm -f "$PLAIN_LOG"' EXIT
LC_ALL=C sed $'s/\033\[[0-9;]*m//g' "$RN_CLI_LOG" > "$PLAIN_LOG"

# The invocation payload carries BITRISE_GIT_MESSAGE, so under debug logging the
# commit message is dumped into the log with the request body — a bare substring
# grep then matches the commit text instead of a real log line. Every TInfof and
# TWarnf line opens with "[HH:MM:SS] ", so anchor on that.
cli_logged() {
  grep -qE "^\[[0-9]{2}:[0-9]{2}:[0-9]{2}\] $1" "$PLAIN_LOG"
}

# --- Invocation IDs ---

if ! cli_logged "React Native invocation ID:"; then
  echo "React Native invocation ID not found in CLI log ❌"
  exit 1
fi
echo "React Native invocation ID present ✅"

# The ID is logged before the PUT, so it proves nothing was saved. Only the
# post-PUT line does, and it is the one assertion that holds without debug logging.
if ! cli_logged "React Native invocation saved\. Visit"; then
  echo "React Native invocation was not saved ❌"
  grep -E "^\[[0-9]{2}:[0-9]{2}:[0-9]{2}\] Failed to send run invocation analytics" "$PLAIN_LOG" || true
  exit 1
fi
echo "React Native invocation saved ✅"

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
# EXPECT_CCACHE=true says this workflow activated ccache and compiled C++, so a
# missing ccache invocation is a regression — most likely the storage helper died
# mid-build, which also takes every later remote cache hit with it — and not the
# "ccache wasn't in play" case the iOS workflow legitimately hits.
EXPECT_CCACHE="${EXPECT_CCACHE:-false}"

if cli_logged "Ccache invocation ID:"; then
  echo "Ccache invocation ID present ✅"

  if ! cli_logged "Parent invocation ID:"; then
    echo "Parent invocation ID not found despite ccache being active ❌"
    exit 1
  fi
  echo "Parent invocation ID present ✅"
elif [ "$EXPECT_CCACHE" = "true" ]; then
  echo "Ccache invocation ID missing although ccache was activated ❌"
  grep -E "Failed to (load session info|get session stats) from storage helper|No ccache activity detected|No invocation ID available for ccache stats" "$PLAIN_LOG" || true
  exit 1
else
  echo "Ccache invocation ID not present (ccache not active or no activity) ℹ️"
  if grep -q "^HTTP PUT:.*/v1/invocations/.*/children/" "$PLAIN_LOG"; then
    echo "Unexpected ccache invocation relation HTTP call found when ccache was inactive ❌"
    exit 1
  fi
  echo "No unexpected ccache relation HTTP calls ✅"
fi

# --- HTTP responses (only when debug logging is active) ---

if grep -q "^HTTP PUT:" "$PLAIN_LOG"; then
  # PutInvocation (react-native run invocation)
  if ! grep -q "^HTTP PUT:.*/v1/invocations/" "$PLAIN_LOG"; then
    echo "No PutInvocation HTTP call found ❌"
    exit 1
  fi
  echo "PutInvocation HTTP call present ✅"

  # PutInvocationRelation (parent→ccache) — only when ccache was activated
  if cli_logged "Ccache invocation ID:"; then
    if ! grep -q "^HTTP PUT:.*/v1/invocations/.*/children/" "$PLAIN_LOG"; then
      echo "No PutInvocationRelation HTTP call found ❌"
      exit 1
    fi
    echo "PutInvocationRelation HTTP call present ✅"
  fi

  # All HTTP responses should be 2xx
  if grep -q "^Response: [^2]" "$PLAIN_LOG"; then
    echo "Non-2xx HTTP response detected ❌"
    grep "^Response: [^2]" "$PLAIN_LOG"
    exit 1
  fi
  echo "All analytics HTTP responses 2xx ✅"
else
  echo "Debug logging not active, skipping HTTP assertions ℹ️"
fi

# --- Failure indicators (should be absent) ---

if cli_logged "Failed to register invocation relation"; then
  echo "Invocation relation registration failed ❌"
  exit 1
fi

# --- Child-stats ledger aggregation ---
# The react-native wrapper aggregates child invocation hit rates at the end of
# its run and reports the mean on its own invocation. The ledger lives under
# ~/.bitrise/cache/invocations/<parent-id>/ and must be cleaned up after.

rn_invocation_id=$(grep -oE "^\[[0-9]{2}:[0-9]{2}:[0-9]{2}\] React Native invocation ID: [a-zA-Z0-9-]+" "$PLAIN_LOG" | head -1 | awk '{print $NF}' || true)

has_child=false
if cli_logged "Ccache invocation ID:"; then
  has_child=true
fi
if [ -n "${XCELERATE_LOGS:-}" ]; then
  has_child=true
fi

if [ "$has_child" = "true" ]; then
  if ! cli_logged "Cache hit rate \(avg of [0-9]+ child invocations\): [0-9]+\.[0-9]+%"; then
    echo "Child hit rate aggregation log line missing ❌"
    exit 1
  fi
  echo "Child hit rate aggregation log line present ✅"

  if cli_logged "Failed to aggregate child invocation hit rates"; then
    echo "Aggregation reported an error ❌"
    exit 1
  fi

  if cli_logged "Failed to write child stats ledger"; then
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
