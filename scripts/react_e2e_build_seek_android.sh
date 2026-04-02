#!/usr/bin/env bash
set -exo pipefail

export RN_CLI_LOG="$BITRISE_DEPLOY_DIR/rn-cli.log"
envman add --key RN_CLI_LOG --value "$RN_CLI_LOG"

cd _seek
../bitrise-build-cache-cli react-native run npx react-native build-android 2>&1 | tee -a "$RN_CLI_LOG"
