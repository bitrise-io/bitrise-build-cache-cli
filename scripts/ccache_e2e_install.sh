#!/bin/bash
set -euxo pipefail

apt-get install -y -q gcc

CCACHE_VERSION="4.13.2"
curl -fsSL "https://github.com/ccache/ccache/releases/download/v${CCACHE_VERSION}/ccache-${CCACHE_VERSION}-linux-x86_64-glibc.tar.gz" \
  | tar -xz --strip-components=1 -C /usr/local/bin "ccache-${CCACHE_VERSION}-linux-x86_64-glibc/ccache"

ccache --version
