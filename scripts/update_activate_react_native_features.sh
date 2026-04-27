#!/bin/bash

set -ex

FILE_TO_UPDATE="step/cli.go"

# bitrise-step-activate-react-native-features keeps the CLI version as a Go
# constant in step/cli.go (downloaded from GitHub releases at runtime, no
# vendored Go module). The constant carries the bare semver — strip the
# leading "v" from BITRISE_GIT_TAG.
NEW_VERSION="${BITRISE_GIT_TAG#v}"

SED_IN_PLACE_COMMAND=(-i)
if [[ "$OSTYPE" == "darwin"* ]]; then
  SED_IN_PLACE_COMMAND=(-i "")
fi

sed -E "${SED_IN_PLACE_COMMAND[@]}" "s/cliVersion[[:space:]]+=[[:space:]]+\"[0-9]+\.[0-9]+\.[0-9]+\"/cliVersion    = \"$NEW_VERSION\"/" "$FILE_TO_UPDATE"
