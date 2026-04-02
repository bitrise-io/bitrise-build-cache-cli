#!/usr/bin/env bash
set -euo pipefail

echo "Asserting react-native CLI analytics log: $RN_CLI_LOG"
cat "$RN_CLI_LOG"

if ! grep -q "Invocation ID:" "$RN_CLI_LOG"; then
  echo "Invocation ID not found in CLI log ❌"
  exit 1
fi
echo "Invocation ID present ✅"

if grep -q "Warning: failed to send run invocation analytics" "$RN_CLI_LOG"; then
  echo "React-native invocation send failed ❌"
  exit 1
fi
echo "React-native invocation sent ✅"

if grep -q "Skipping ccache stats reset because collection/send failed" "$RN_CLI_LOG"; then
  echo "Ccache invocation send failed ❌"
  exit 1
fi
echo "Ccache invocation sent ✅"

if grep -q "Warning: failed to register invocation relation" "$RN_CLI_LOG"; then
  echo "Invocation relation registration failed ❌"
  exit 1
fi
echo "Invocation relation registered ✅"
