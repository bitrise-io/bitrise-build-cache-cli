#!/usr/bin/env bash
set -euxo pipefail

CCACHE_VERSION="4.13.2"
curl -fsSL "https://github.com/ccache/ccache/releases/download/v${CCACHE_VERSION}/ccache-${CCACHE_VERSION}-darwin.tar.gz" \
  | tar -xz --strip-components=1 -C /usr/local/bin "ccache-${CCACHE_VERSION}-darwin/ccache"

ccache --version
