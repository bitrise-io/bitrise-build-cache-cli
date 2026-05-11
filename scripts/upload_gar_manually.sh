#!/usr/bin/env bash
set -euo pipefail

if [ "$TAG" == "" ]; then
  echo "TAG environment variable is not set. Exiting."
  exit 1
fi


DIST_DIR=dist

git tag --delete "$TAG" || true
git tag $TAG

rm -rf dist && goreleaser release --skip=publish

clean_tag="${TAG#v}"

filenames=(
  "bitrise-build-cache_${clean_tag}_darwin_amd64.tar.gz"
  "bitrise-build-cache_${clean_tag}_linux_amd64.tar.gz"
  "bitrise-build-cache_${clean_tag}_darwin_arm64.tar.gz"
  "bitrise-build-cache_${clean_tag}_linux_arm64.tar.gz"
  "bitrise-build-cache_${clean_tag}_checksums.txt"
)

for filename in "${filenames[@]}"; do
  if [[ ! -f "$DIST_DIR/$filename" ]]; then
    echo "File $DIST_DIR/$filename does not exist."
    exit 1
  fi

  package_name="${filename/_${clean_tag}/}"

  echo "Uploading $filename to GAR as package $package_name..."

  gcloud artifacts files delete "$package_name:$clean_tag:$filename" \
    --project=ip-build-cache-prod \
    --location=us-central1 \
    --repository=build-cache-cli-releases \
    --quiet || true
  gcloud artifacts generic upload \
    --project=ip-build-cache-prod \
    --source="$DIST_DIR/$filename" \
    --package="$package_name" \
    --version="$clean_tag" \
    --location=us-central1 \
    --repository=build-cache-cli-releases
done

echo "Uploaded CLI artifacts to GAR."

# Mirror the pinned ccache release to GAR so the CLI installer can pull it
# from there as a fallback after the upstream GitHub release.
CCACHE_VERSION=$(grep -E '^[[:space:]]*ccacheVersion[[:space:]]*=[[:space:]]*"' internal/dependencies/ccache.go | sed -E 's/.*"([^"]+)".*/\1/')
if [[ -z "$CCACHE_VERSION" ]]; then
  echo "Failed to extract ccache version from internal/dependencies/ccache.go"
  exit 1
fi

CCACHE_TMP=$(mktemp -d)
trap 'rm -rf "$CCACHE_TMP"' EXIT

for suffix in darwin linux-x86_64-glibc linux-aarch64-glibc; do
  filename="ccache-${CCACHE_VERSION}-${suffix}.tar.gz"
  package="ccache-${suffix}.tar.gz"
  src_url="https://github.com/ccache/ccache/releases/download/v${CCACHE_VERSION}/${filename}"

  echo "Downloading $src_url"
  curl --retry 5 -sSfL -o "$CCACHE_TMP/$filename" "$src_url"

  gcloud artifacts files delete "$package:$CCACHE_VERSION:$filename" \
    --project=ip-build-cache-prod \
    --location=us-central1 \
    --repository=build-cache-cli-releases \
    --quiet || true

  gcloud artifacts generic upload \
    --project=ip-build-cache-prod \
    --source="$CCACHE_TMP/$filename" \
    --package="$package" \
    --version="$CCACHE_VERSION" \
    --location=us-central1 \
    --repository=build-cache-cli-releases
done

echo "Mirrored ccache v${CCACHE_VERSION} to GAR."
