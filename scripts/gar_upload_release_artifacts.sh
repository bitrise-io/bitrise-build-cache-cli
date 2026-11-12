#!/usr/bin/env bash
# Upload the goreleaser-produced platform tarballs + checksums to the
# `build-cache-cli-releases` GAR generic repo, pinned to the release tag.
# Idempotent: describe-or-upload (never delete; #327 immutability rule).
#
# Required env:
#   BITRISE_GIT_TAG     — the release tag (e.g. "v2.6.4"). Leading "v"
#                         is stripped before use as the GAR version.
set -eo pipefail

DIST_DIR="dist"
if [[ ! -d "$DIST_DIR" ]]; then
  echo "Directory $DIST_DIR does not exist."
  exit 1
fi
# Portable across stacks (`tree` is not in default osx-xcode-edge).
find dist -maxdepth 2 | sort

tag="${BITRISE_GIT_TAG#v}" # Remove the v from the tag
if [[ -z "$tag" ]]; then
  echo "BITRISE_GIT_TAG is not set. Exiting."
  exit 1
fi

echo "Uploading with tag: $tag"

filenames=("bitrise-build-cache_${tag}_darwin_amd64.tar.gz"
          "bitrise-build-cache_${tag}_linux_amd64.tar.gz"
          "bitrise-build-cache_${tag}_darwin_arm64.tar.gz"
          "bitrise-build-cache_${tag}_linux_arm64.tar.gz"
          "bitrise-build-cache_${tag}_checksums.txt")
for filename in "${filenames[@]}"; do
  if [[ ! -f "$DIST_DIR/$filename" ]]; then
    echo "File $DIST_DIR/$filename does not exist."
    exit 1
  fi
  package_name="${filename/_${tag}/}" # Files will be versioned

  if gcloud artifacts files describe "$package_name:$tag:$filename" \
       --project=ip-build-cache-prod \
       --location=us-central1 \
       --repository=build-cache-cli-releases \
       >/dev/null 2>&1; then
    echo "Already uploaded, skipping: $filename"
    continue
  fi

  echo "Uploading $filename to GAR..."
  gcloud artifacts generic upload \
    --project=ip-build-cache-prod \
    --source="$DIST_DIR/$filename" \
    --package="$package_name" \
    --version="$tag" \
    --location=us-central1 \
    --repository=build-cache-cli-releases
done

# Trampoline binaries + checksums.
# Mirrors the R2 upload so GAR carries the same per-version artefact set as
# the CLI binaries (consumers today read R2; adding GAR keeps the pinned
# audit trail consistent should the download client learn to fall back).
TRAMPOLINE_DIR="$DIST_DIR/trampoline"
if [[ ! -d "$TRAMPOLINE_DIR" ]]; then
  echo "Trampoline directory $TRAMPOLINE_DIR missing — build_trampoline_universal.sh must run before this script."
  exit 1
fi

trampoline_files=("trampoline_v${tag}_darwin_amd64"
                  "trampoline_v${tag}_darwin_arm64"
                  "trampoline_v${tag}_darwin_universal"
                  "trampoline_v${tag}_checksums.txt")
for filename in "${trampoline_files[@]}"; do
  src="$TRAMPOLINE_DIR/$filename"
  if [[ ! -f "$src" ]]; then
    echo "Trampoline file $src missing."
    exit 1
  fi
  package_name="${filename/_v${tag}/}" # Files are versioned via the tag.

  if gcloud artifacts files describe "$package_name:$tag:$filename" \
       --project=ip-build-cache-prod \
       --location=us-central1 \
       --repository=build-cache-cli-releases \
       >/dev/null 2>&1; then
    echo "Already uploaded, skipping: $filename"
    continue
  fi

  echo "Uploading $filename to GAR..."
  gcloud artifacts generic upload \
    --project=ip-build-cache-prod \
    --source="$src" \
    --package="$package_name" \
    --version="$tag" \
    --location=us-central1 \
    --repository=build-cache-cli-releases
done

echo "Uploaded artifacts to GAR."
