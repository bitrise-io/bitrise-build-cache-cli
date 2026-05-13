#!/usr/bin/env bash
# Mirror install/installer.sh to GAR so consumers can fall back when
# raw.githubusercontent.com or github.com is degraded. Two views are
# maintained:
#
#   installer.sh:<tag>:installer.sh           — IMMUTABLE pinned copy
#                                               (audit trail; describe-
#                                               or-upload, never delete)
#   installer.sh:latest-pointer:installer.sh  — MUTABLE pointer to the
#                                               newest release's script
#   installer.sh:latest-pointer:VERSION       — bare semver string, read
#                                               by installer.sh's GAR
#                                               fallback (gar_latest_version)
#                                               for tag resolution when
#                                               github.com is unreachable
#
# Note: GAR rejects the literal version_id "latest" as reserved, so the
# mutable view uses "latest-pointer".
#
# The :latest-pointer view is the *documented* carve-out from the #327
# "never delete-then-upload" rule. Safe because the mutable view is only
# hit when the primary (github.com / raw.githubusercontent.com) is already
# failing — i.e. an already-degraded path, not a hot path.
#
# Required env:
#   BITRISE_GIT_TAG  — the release tag (e.g. "v2.6.4"); the leading "v"
#                      is stripped before use as the GAR version.
set -euo pipefail

tag="${BITRISE_GIT_TAG#v}"
if [[ -z "$tag" ]]; then
  echo "BITRISE_GIT_TAG is not set. Exiting."
  exit 1
fi

package="installer.sh"
source_file="install/installer.sh"
gar_args=(--project=ip-build-cache-prod
          --location=us-central1
          --repository=build-cache-cli-releases)

if [[ ! -f "$source_file" ]]; then
  echo "Source file $source_file does not exist."
  exit 1
fi

# ---- 1. Immutable pinned copy (describe-or-upload) ----
if gcloud artifacts files describe "$package:$tag:installer.sh" "${gar_args[@]}" >/dev/null 2>&1; then
  echo "installer.sh already mirrored for tag $tag, skipping pinned upload"
else
  echo "Uploading $source_file to GAR as $package:$tag:installer.sh..."
  gcloud artifacts generic upload \
    --source="$source_file" \
    --package="$package" \
    --version="$tag" \
    "${gar_args[@]}"
fi

# ---- 2. Mutable :latest-pointer (delete-then-upload) ----
# Refresh installer.sh
if gcloud artifacts files describe "$package:latest-pointer:installer.sh" "${gar_args[@]}" >/dev/null 2>&1; then
  echo "Deleting stale $package:latest-pointer:installer.sh before re-upload..."
  gcloud artifacts files delete "$package:latest-pointer:installer.sh" --quiet "${gar_args[@]}"
fi
echo "Uploading $source_file as $package:latest-pointer:installer.sh..."
gcloud artifacts generic upload \
  --source="$source_file" \
  --package="$package" \
  --version="latest-pointer" \
  "${gar_args[@]}"

# Refresh VERSION (bare semver, no leading v). gcloud derives the GAR
# filename from the basename of --source, so the file on disk must
# literally be named "VERSION".
version_dir="$(mktemp -d)"
trap 'rm -rf "$version_dir"' EXIT
printf '%s' "$tag" > "$version_dir/VERSION"
if gcloud artifacts files describe "$package:latest-pointer:VERSION" "${gar_args[@]}" >/dev/null 2>&1; then
  echo "Deleting stale $package:latest-pointer:VERSION before re-upload..."
  gcloud artifacts files delete "$package:latest-pointer:VERSION" --quiet "${gar_args[@]}"
fi
echo "Uploading VERSION ($tag) as $package:latest-pointer:VERSION..."
gcloud artifacts generic upload \
  --source="$version_dir/VERSION" \
  --package="$package" \
  --version="latest-pointer" \
  "${gar_args[@]}"

echo "Mirrored installer.sh to GAR (pinned $tag + :latest-pointer)."
