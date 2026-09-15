#!/usr/bin/env bash
# @react-native-community/cli's installApp locates the built .app by running
# `xcodebuild -showBuildSettings` and joining TARGET_BUILD_DIR with
# EXECUTABLE_FOLDER_PATH. The wrapper relocates DerivedData, so unless the query
# is told about it too, the install resolves to a path the build never wrote.
set -exo pipefail

: "${BITRISE_XCODE_DERIVED_DATA_PATH:?activate must export the DerivedData root}"

cd _seek/ios

workspace=$(echo ./*.xcworkspace)
workspace=$(basename "$workspace")
scheme="${workspace%.xcworkspace}"

# The probe RN runs in getBuildSettings, flag for flag — note no -derivedDataPath.
xcodebuild -showBuildSettings -json \
  -workspace "$workspace" \
  -scheme "$scheme" \
  -sdk iphonesimulator \
  -configuration Debug > "$BITRISE_DEPLOY_DIR/build-settings.json"

settings=$(jq -r '[.[] | select(.buildSettings.WRAPPER_EXTENSION == "app")][0].buildSettings' \
  "$BITRISE_DEPLOY_DIR/build-settings.json")
[ "$settings" != "null" ] || { echo "FAIL: no app target in -showBuildSettings output"; exit 1; }

target_build_dir=$(jq -r '.TARGET_BUILD_DIR' <<<"$settings")
echo "TARGET_BUILD_DIR=$target_build_dir"

case "$target_build_dir" in
"$BITRISE_XCODE_DERIVED_DATA_PATH"/*) ;;
*)
  echo "FAIL: query answered $target_build_dir, outside $BITRISE_XCODE_DERIVED_DATA_PATH"
  exit 1
  ;;
esac
