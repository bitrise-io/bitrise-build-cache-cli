#!/usr/bin/env bash
set -euo pipefail

echo "Asserting on react-native CLI log: $RN_CLI_LOG"
cat "$RN_CLI_LOG"

# The wrapper must have printed the invocation ID before executing the build.
if ! grep -q "Invocation ID:" "$RN_CLI_LOG"; then
  echo "Invocation ID not found in CLI log ❌"
  exit 1
fi
echo "Invocation ID present ✅"

# The wrapper must have successfully sent the run invocation to the analytics backend.
if ! grep -q "Run invocation sent (id=" "$RN_CLI_LOG"; then
  echo "Run invocation was not sent successfully ❌"
  exit 1
fi
echo "Run invocation sent ✅"
