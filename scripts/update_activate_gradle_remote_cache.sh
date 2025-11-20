#!/bin/bash

set -ex

FILE_TO_UPDATE="step.sh"

# Update the version in the file
SED_IN_PLACE_COMMAND=(-i)
if [[ "$OSTYPE" == "darwin"* ]]; then
  SED_IN_PLACE_COMMAND=(-i "")
fi

sed -E "${SED_IN_PLACE_COMMAND[@]}" "s/export BITRISE_BUILD_CACHE_CLI_VERSION=\"v?[0-9]+\.[0-9]+\.[0-9]+\"/export BITRISE_BUILD_CACHE_CLI_VERSION=\"$BITRISE_GIT_TAG\"/" "$FILE_TO_UPDATE"