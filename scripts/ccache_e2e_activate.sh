#!/bin/bash
set -euxo pipefail

# Writes ~/.bitrise/cache/ccache/config.json (socket path, auth, endpoint) and
# exports CCACHE_REMOTE_STORAGE / CCACHE_REMOTE_ONLY via envman for the next step.
./bitrise-build-cache-cli activate c++
