#!/usr/bin/env bash
# Publish the Homebrew formula to bitrise-io/homebrew-bitrise-build-cache.
#
# Decoupled from goreleaser: reads SHA256s from dist/checksums.txt (already
# written by the goreleaser step earlier in the release workflow) and
# templates Formula/bitrise-build-cache.rb directly, then pushes to the tap.
# This avoids a second goreleaser invocation that would rebuild + re-upload
# all binaries to the existing GH release.
#
# Inputs (env):
#   BITRISE_GIT_TAG   release tag, e.g. v2.6.2 (or pass TAG to override)
#   GITHUB_TOKEN      PAT with push access to bitrise-io/homebrew-bitrise-build-cache
#
# Optional (env):
#   DIST_DIR          directory containing checksums.txt (default: dist)
#   TAP_REMOTE        tap repo URL (default: bitrise-io/homebrew-bitrise-build-cache)
#   TAP_BRANCH        tap branch to push to (default: main)
#   DRY_RUN           if "1", template the formula and print it; do not push

set -euo pipefail

TAG="${TAG:-${BITRISE_GIT_TAG:-}}"
[ -n "$TAG" ] || { echo "TAG / BITRISE_GIT_TAG is not set" >&2; exit 1; }
VER="${TAG#v}"

DIST_DIR="${DIST_DIR:-dist}"
TAP_BRANCH="${TAP_BRANCH:-main}"
TAP_REMOTE_DEFAULT="bitrise-io/homebrew-bitrise-build-cache"
DRY_RUN="${DRY_RUN:-0}"

CHECKSUMS="${DIST_DIR}/bitrise-build-cache_${VER}_checksums.txt"
[ -f "$CHECKSUMS" ] || { echo "missing $CHECKSUMS" >&2; exit 1; }

sha_for() {
  local name="bitrise-build-cache_${VER}_$1.tar.gz"
  local sha
  sha=$(awk -v n="$name" '$2==n {print $1}' "$CHECKSUMS")
  [ -n "$sha" ] || { echo "no sha for $name in $CHECKSUMS" >&2; exit 1; }
  echo "$sha"
}

SHA_DARWIN_AMD64=$(sha_for darwin_amd64)
SHA_DARWIN_ARM64=$(sha_for darwin_arm64)
SHA_LINUX_AMD64=$(sha_for linux_amd64)
SHA_LINUX_ARM64=$(sha_for linux_arm64)

URL_BASE="https://github.com/bitrise-io/bitrise-build-cache-cli/releases/download/${TAG}"

render_formula() {
  cat <<RUBY
class BitriseBuildCache < Formula
  desc "Bitrise Build Cache CLI — configure remote build cache for Gradle, Bazel, Xcode, and React Native"
  homepage "https://bitrise.io"
  version "${VER}"
  license "MIT"

  on_macos do
    on_arm do
      url "${URL_BASE}/bitrise-build-cache_${VER}_darwin_arm64.tar.gz"
      sha256 "${SHA_DARWIN_ARM64}"
    end
    on_intel do
      url "${URL_BASE}/bitrise-build-cache_${VER}_darwin_amd64.tar.gz"
      sha256 "${SHA_DARWIN_AMD64}"
    end
  end

  on_linux do
    on_arm do
      url "${URL_BASE}/bitrise-build-cache_${VER}_linux_arm64.tar.gz"
      sha256 "${SHA_LINUX_ARM64}"
    end
    on_intel do
      url "${URL_BASE}/bitrise-build-cache_${VER}_linux_amd64.tar.gz"
      sha256 "${SHA_LINUX_AMD64}"
    end
  end

  def install
    bin.install "bitrise-build-cache"
  end

  test do
    system "#{bin}/bitrise-build-cache", "--help"
  end
end
RUBY
}

if [ "$DRY_RUN" = "1" ]; then
  render_formula
  exit 0
fi

[ -n "${GITHUB_TOKEN:-}" ] || { echo "GITHUB_TOKEN is not set" >&2; exit 1; }
TAP_REMOTE="${TAP_REMOTE:-https://x-access-token:${GITHUB_TOKEN}@github.com/${TAP_REMOTE_DEFAULT}.git}"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

git clone --depth 1 --branch "$TAP_BRANCH" "$TAP_REMOTE" "$WORK"
cd "$WORK"
git config user.name "bitbot"
git config user.email "letsbuild@bitrise.io"

mkdir -p Formula
render_formula > Formula/bitrise-build-cache.rb

git add Formula/bitrise-build-cache.rb
if git diff --cached --quiet; then
  echo "Formula content unchanged; nothing to commit"
  exit 0
fi
git commit -m "brew formula update for bitrise-build-cache ${TAG}"
git push origin "HEAD:${TAP_BRANCH}"
