#!/usr/bin/env bash
set -euxo pipefail

git clone --depth 1 "$SEEK_URL" --branch "$SEEK_TAG" _seek
