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

echo "Uploaded artifacts to GAR."
