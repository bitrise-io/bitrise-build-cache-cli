#!/usr/bin/env bash
# Upload the goreleaser-produced platform tarballs + checksums to the
# Cloudflare R2 bucket `build-cache-cli-releases`. The bucket feeds the
# preboot host-VM cache proxy (served at
# http://${SUBNET_IP}1:59020/build-cache-cli-releases/<tarball>) so the
# layout MUST be flat: object key == filename (no `/<tag>/` prefix).
#
# Idempotent: head-object describe-or-upload (never delete; #327
# immutability rule).
#
# Required env:
#   BITRISE_GIT_TAG       — the release tag (e.g. "v2.6.4"). Leading "v"
#                           is stripped before use.
#   R2_ACCOUNT_ID         — Cloudflare account ID (a484c7653eeba8c8c00a4bf3967860a3).
#   R2_ACCESS_KEY_ID      — R2 API token access key (write-only scope on bucket).
#   R2_SECRET_ACCESS_KEY  — R2 API token secret.
set -eo pipefail

DIST_DIR="dist"
BUCKET="build-cache-cli-releases"

if [[ ! -d "$DIST_DIR" ]]; then
  echo "Directory $DIST_DIR does not exist."
  exit 1
fi

tag="${BITRISE_GIT_TAG#v}"
if [[ -z "$tag" ]]; then
  echo "BITRISE_GIT_TAG is not set. Exiting."
  exit 1
fi

for v in R2_ACCOUNT_ID R2_ACCESS_KEY_ID R2_SECRET_ACCESS_KEY; do
  if [[ -z "${!v:-}" ]]; then
    echo "$v is not set. Exiting."
    exit 1
  fi
done

# Map R2 creds onto the standard AWS env vars the CLI reads.
export AWS_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID"
export AWS_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY"
export AWS_DEFAULT_REGION="auto"
# R2 rejects AWS CLI v2's default flexible checksums; force the legacy
# behaviour so PutObject succeeds.
export AWS_REQUEST_CHECKSUM_CALCULATION="when_required"
export AWS_RESPONSE_CHECKSUM_VALIDATION="when_required"

ENDPOINT="https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com"

echo "Uploading release ${tag} artifacts to R2 bucket ${BUCKET}"

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

  if aws s3api head-object \
       --endpoint-url "$ENDPOINT" \
       --bucket "$BUCKET" \
       --key "$filename" \
       >/dev/null 2>&1; then
    echo "Already uploaded, skipping: $filename"
    continue
  fi

  echo "Uploading $filename to R2..."
  aws s3api put-object \
    --endpoint-url "$ENDPOINT" \
    --bucket "$BUCKET" \
    --key "$filename" \
    --body "$DIST_DIR/$filename"
done

echo "Uploaded ${tag} artifacts to R2."
