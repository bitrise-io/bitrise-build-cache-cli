#!/usr/bin/env bash
set -euo pipefail

echo "Asserting react-native CLI analytics log: $RN_CLI_LOG"

# --- Invocation IDs ---

if ! grep -q "React Native invocation ID:" "$RN_CLI_LOG"; then
  echo "React Native invocation ID not found in CLI log ❌"
  exit 1
fi
echo "React Native invocation ID present ✅"

if grep -q "Ccache invocation ID:" "$RN_CLI_LOG"; then
  echo "Ccache invocation ID present ✅"

  if ! grep -q "Parent invocation ID:" "$RN_CLI_LOG"; then
    echo "Parent invocation ID not found despite ccache being active ❌"
    exit 1
  fi
  echo "Parent invocation ID present ✅"
else
  echo "Ccache invocation ID not present (C++ cache not activated, skipping ccache checks) ℹ️"
fi

# --- HTTP responses (requires --debug) ---

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

# --- Failure indicators (should be absent) ---

if grep -q "Warning: failed to send run invocation analytics" "$RN_CLI_LOG"; then
  echo "React-native invocation send failed ❌"
  exit 1
fi

if grep -q "Warning: failed to register invocation relation" "$RN_CLI_LOG"; then
  echo "Invocation relation registration failed ❌"
  exit 1
fi

echo "All analytics assertions passed ✅"
