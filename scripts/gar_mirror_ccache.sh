#!/usr/bin/env bash
# Mirror the pinned ccache binaries to GAR so the CLI's ccache install
# flow has a fallback that doesn't depend on github.com/ccache/ccache.
# Idempotent: describe-or-upload.
set -euo pipefail

# Pull the pinned ccache version straight from the source so the
# mirror always matches what the CLI's installer expects.
CCACHE_VERSION=$(grep -E '^[[:space:]]*ccacheVersion[[:space:]]*=[[:space:]]*"' internal/dependencies/ccache.go | sed -E 's/.*"([^"]+)".*/\1/')
if [[ -z "$CCACHE_VERSION" ]]; then
  echo "Failed to extract ccache version from internal/dependencies/ccache.go"
  exit 1
fi
echo "Mirroring ccache v${CCACHE_VERSION} to GAR..."

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

for suffix in darwin linux-x86_64-glibc linux-aarch64-glibc; do
  filename="ccache-${CCACHE_VERSION}-${suffix}.tar.gz"
  package="ccache-${suffix}.tar.gz"

  if gcloud artifacts files describe "$package:$CCACHE_VERSION:$filename" \
       --project=ip-build-cache-prod \
       --location=us-central1 \
       --repository=build-cache-cli-releases \
       >/dev/null 2>&1; then
    echo "ccache ${CCACHE_VERSION} ${suffix} already mirrored, skipping"
    continue
  fi

  src_url="https://github.com/ccache/ccache/releases/download/v${CCACHE_VERSION}/${filename}"
  echo "Downloading $src_url"
  curl --retry 5 -sSfL -o "$TMPDIR/$filename" "$src_url"

  gcloud artifacts generic upload \
    --project=ip-build-cache-prod \
    --source="$TMPDIR/$filename" \
    --package="$package" \
    --version="$CCACHE_VERSION" \
    --location=us-central1 \
    --repository=build-cache-cli-releases
done

echo "Mirrored ccache to GAR."
