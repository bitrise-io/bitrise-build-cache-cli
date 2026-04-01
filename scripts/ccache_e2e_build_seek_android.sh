#!/usr/bin/env bash
set -exo pipefail

cd _seek
../bitrise-build-cache-cli react-native run npx react-native build-android
