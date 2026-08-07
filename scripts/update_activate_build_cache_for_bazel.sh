#!/bin/bash

set -ex

# The bazel step installs the CLI by pinned version instead of vendoring it as a
# Go module, so the bump is a single line in step.sh rather than a `go get`.
# Written via a temp file because `sed -i` takes a backup suffix on BSD sed.
tmp="$(mktemp)"
sed -E "s|^(export BITRISE_BUILD_CACHE_CLI_VERSION=\")[^\"]+(\")|\1${BITRISE_GIT_TAG}\2|" step.sh > "$tmp"
mv "$tmp" step.sh

# Guard against a regex that matched nothing or matched too much.
actual_diff="$(git diff --numstat)"
if [ "$actual_diff" != "1	1	step.sh" ]; then
  echo "Expected exactly one changed line in step.sh, got:"
  echo "$actual_diff"
  exit 1
fi
